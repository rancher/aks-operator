package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	wranglerv1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/rancher/aks-operator/pkg/aks"
	"github.com/rancher/aks-operator/pkg/aks/services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	akscontrollers "github.com/rancher/aks-operator/pkg/generated/controllers/aks.cattle.io/v1"
	"github.com/rancher/aks-operator/pkg/utils"
)

const (
	aksClusterConfigKind     = "AKSClusterConfig"
	controllerName           = "aks-controller"
	controllerRemoveName     = "aks-controller-remove"
	aksConfigCreatingPhase   = "creating"
	aksConfigNotCreatedPhase = ""
	aksConfigActivePhase     = "active"
	aksConfigUpdatingPhase   = "updating"
	aksConfigImportingPhase  = "importing"
	poolNameMaxLength        = 6
	wait                     = 30
)

// Cluster Status
const (
	// ClusterStatusSucceeded The Succeeeded state indicates the cluster has been
	// created and is fully usable, return code 0
	ClusterStatusSucceeded = "Succeeded"

	// ClusterStatusFailed The Failed state indicates the cluster is unusable, return code 1
	ClusterStatusFailed = "Failed"

	// ClusterStatusInProgress The InProgress state indicates that some work is
	// actively being done on the cluster, such as upgrading the master or
	// node software, return code 3
	ClusterStatusInProgress = "InProgress"

	// ClusterStatusUpdating The Updating state indicates the cluster is updating
	ClusterStatusUpdating = "Updating"

	// ClusterStatusCanceled The Canceled state indicates that create or update was canceled, return code 2
	ClusterStatusCanceled = "Canceled"

	// ClusterStatusDeleting The Deleting state indicates that cluster was removed, return code 4
	ClusterStatusDeleting = "Deleting"

	// NodePoolSucceeded The Succeeeded state indicates the node pool has been
	// created and is fully usable, return code 0
	NodePoolSucceeded = "Succeeded"

	// NodePoolCreating The Creating state indicates that node pool is creating
	NodePoolCreating = "Creating"

	// NodePoolScaling The Scaling state indicates that node pool is being scaled
	NodePoolScaling = "Scaling"

	// NodePoolDeleting The Deleting state indicates that node pool is being deleted
	NodePoolDeleting = "Deleting"

	// NodePoolUpgrading The Upgrading state indicates that node pool is upgrading
	NodePoolUpgrading = "Upgrading"
)

var matchWorkspaceGroup = regexp.MustCompile("/(?i)resourcegroups/(.+?)/")
var matchWorkspaceName = regexp.MustCompile("/(?i)workspaces/(.+?)$")

type Handler struct {
	aksCC           akscontrollers.AKSClusterConfigClient
	aksEnqueueAfter func(namespace, name string, duration time.Duration)
	aksEnqueue      func(namespace, name string)
	azureClients    azureClients
	secrets         wranglerv1.SecretClient
	secretsCache    wranglerv1.SecretCache
}

type azureClients struct {
	credentials aks.Credentials

	clustersClient       services.ManagedClustersClientInterface
	resourceGroupsClient services.ResourceGroupsClientInterface
	agentPoolsClient     services.AgentPoolsClientInterface
	workplacesClient     services.WorkplacesClientInterface
}

func Register(
	ctx context.Context,
	secrets wranglerv1.SecretController,
	aks akscontrollers.AKSClusterConfigController) {
	controller := &Handler{
		aksCC:           aks,
		aksEnqueue:      aks.Enqueue,
		aksEnqueueAfter: aks.EnqueueAfter,
		secretsCache:    secrets.Cache(),
		secrets:         secrets,
	}

	// Register handlers
	aks.OnChange(ctx, controllerName, controller.recordError(controller.OnAksConfigChanged))
	aks.OnRemove(ctx, controllerRemoveName, controller.OnAksConfigRemoved)
}

func (h *Handler) OnAksConfigChanged(_ string, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	if config == nil || config.DeletionTimestamp != nil {
		return nil, nil
	}

	if err := h.getAzureClients(config); err != nil {
		return config, fmt.Errorf("error getting Azure clients: %w", err)
	}

	switch config.Status.Phase {
	case aksConfigImportingPhase:
		return h.importCluster(config)
	case aksConfigNotCreatedPhase:
		return h.createCluster(config)
	case aksConfigCreatingPhase:
		return h.waitForCluster(config)
	case aksConfigActivePhase, aksConfigUpdatingPhase:
		return h.checkAndUpdate(config)
	default:
		return config, fmt.Errorf("invalid phase: %v", config.Status.Phase)
	}
}

func (h *Handler) OnAksConfigRemoved(_ string, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	if err := h.getAzureClients(config); err != nil {
		return config, fmt.Errorf("error getting Azure clients: %w", err)
	}

	if config.Spec.Imported {
		logrus.Infof("Cluster [%s (id: %s)] is imported, will not delete AKS cluster", config.Spec.ClusterName, config.Name)
		return config, nil
	}
	if config.Status.Phase == aksConfigNotCreatedPhase {
		// The most likely context here is that the cluster already existed in AKS, so we shouldn't delete it
		logrus.Warnf("Cluster [%s (id: %s)] never advanced to creating status, will not delete AKS cluster", config.Spec.ClusterName, config.Name)
		return config, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logrus.Infof("Removing cluster [%s (id: %s)]", config.Spec.ClusterName, config.Name)

	clusterExists, err := aks.ExistsCluster(ctx, h.azureClients.clustersClient, &config.Spec)
	if err != nil && strings.Contains(err.Error(), "unauthorized") {
		logrus.Infof("User does not have permissions to access cluster [%s (id: %s)]: %s", config.Spec.ClusterName, config.Name, err)
	}

	if clusterExists {
		if err = aks.RemoveCluster(ctx, h.azureClients.clustersClient, &config.Spec); err != nil {
			return config, fmt.Errorf("error removing cluster [%s (id: %s)] message %v", config.Spec.ClusterName, config.Name, err)
		}
	}

	logrus.Infof("Cluster [%s (id: %s)] was removed successfully", config.Spec.ClusterName, config.Name)
	logrus.Infof("Resource group [%s] for cluster [%s (id: %s)] still exists, please remove it if needed", config.Spec.ResourceGroup, config.Spec.ClusterName, config.Name)

	return config, nil
}

// recordError writes the error return by onChange to the failureMessage field on status. If there is no error, then
// empty string will be written to status
func (h *Handler) recordError(onChange func(key string, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error)) func(key string, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	return func(key string, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
		var err error
		var message string
		config, err = onChange(key, config)
		if config == nil {
			// AKS config is likely deleting
			return config, err
		}
		if err != nil {
			if apierrors.IsConflict(err) {
				// conflict error means the config is updated by rancher controller
				// the changes which needs to be done by the operator controller will be handled in next
				// reconcile call
				logrus.Debugf("Error updating aksclusterconfig: %s", err.Error())
				return config, err
			}

			message = err.Error()
		}

		if config.Status.FailureMessage == message {
			return config, err
		}

		config = config.DeepCopy()
		if message != "" && config.Status.Phase == aksConfigActivePhase {
			// can assume an update is failing
			config.Status.Phase = aksConfigUpdatingPhase
		}
		config.Status.FailureMessage = message

		var recordErr error
		config, recordErr = h.aksCC.UpdateStatus(config)
		if recordErr != nil {
			logrus.Errorf("Error recording akscc [%s (id: %s)] failure message: %s", config.Spec.ClusterName, config.Name, recordErr.Error())
		}
		return config, err
	}
}

func (h *Handler) createCluster(config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	if err := h.validateConfig(config); err != nil {
		return config, err
	}

	if config.Spec.Imported {
		config = config.DeepCopy()
		config.Status.Phase = aksConfigImportingPhase
		return h.aksCC.UpdateStatus(config)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logrus.Infof("Creating cluster [%s (id: %s)]", config.Spec.ClusterName, config.Name)

	logrus.Infof("Checking if cluster [%s (id: %s)] exists", config.Spec.ClusterName, config.Name)

	clusterExists, err := aks.ExistsCluster(ctx, h.azureClients.clustersClient, &config.Spec)
	if err != nil && strings.Contains(err.Error(), "unauthorized") {
		logrus.Infof("User does not have permissions to access cluster [%s (id: %s)]: %s", config.Spec.ClusterName, config.Name, err)
	}

	if clusterExists {
		return config, fmt.Errorf("cluster [%s (id: %s)] already exists in AKS. Update configuration or import the existing one", config.Spec.ClusterName, config.Name)
	}

	logrus.Infof("Checking if resource group [%s] exists", config.Spec.ResourceGroup)

	resourceGroupExists, err := aks.ExistsResourceGroup(ctx, h.azureClients.resourceGroupsClient, config.Spec.ResourceGroup)
	if err != nil && strings.Contains(err.Error(), "unauthorized") {
		logrus.Infof("User does not have permissions to access resource group [%s]: %s", config.Spec.ResourceGroup, err)
	}

	if !resourceGroupExists {
		logrus.Infof("Creating resource group [%s] for cluster [%s (id: %s)]", config.Spec.ResourceGroup, config.Spec.ClusterName, config.Name)
		err = aks.CreateResourceGroup(ctx, h.azureClients.resourceGroupsClient, &config.Spec)
		if err != nil {
			return config, fmt.Errorf("error creating resource group [%s] with message %v", config.Spec.ResourceGroup, err)
		}
		logrus.Infof("Resource group [%s] created successfully", config.Spec.ResourceGroup)
	}

	logrus.Infof("Creating AKS cluster [%s (id: %s)]", config.Spec.ClusterName, config.Name)

	err = aks.CreateCluster(ctx, &h.azureClients.credentials, h.azureClients.clustersClient, h.azureClients.workplacesClient, &config.Spec, config.Status.Phase)
	if err != nil {
		return config, fmt.Errorf("error failed to create cluster: %v ", err)
	}

	config = config.DeepCopy()
	config.Status.Phase = aksConfigCreatingPhase
	return h.aksCC.UpdateStatus(config)
}

func (h *Handler) importCluster(config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logrus.Infof("Importing config for cluster [%s (id: %s)]", config.Spec.ClusterName, config.Name)

	if err := h.createCASecret(ctx, config); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return config, err
		}
	}

	config = config.DeepCopy()
	config.Status.Phase = aksConfigActivePhase
	return h.aksCC.UpdateStatus(config)
}

func (h *Handler) checkAndUpdate(config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := h.azureClients.clustersClient.Get(ctx, config.Spec.ResourceGroup, config.Spec.ClusterName, nil)
	if err != nil {
		return config, err
	}

	if config.Status.RBACEnabled == nil && result.Properties.EnableRBAC != nil {
		config = config.DeepCopy()
		config.Status.RBACEnabled = result.Properties.EnableRBAC
		return h.aksCC.UpdateStatus(config)
	}

	clusterState := *result.Properties.ProvisioningState
	if clusterState == ClusterStatusFailed {
		return config, fmt.Errorf("update failed for cluster [%s (id: %s)], status: %s", config.Spec.ClusterName, config.Name, clusterState)
	}
	if clusterState == ClusterStatusInProgress || clusterState == ClusterStatusUpdating {
		// If the cluster is in an active state in Rancher but is updating in AKS, then an update was initiated outside of Rancher,
		// such as in AKS console. In this case, this is a no-op and the reconciliation will happen after syncing.
		if config.Status.Phase == aksConfigActivePhase {
			logrus.Infof("Waiting for non-Rancher initiated cluster update for [%s (id: %s)]", config.Spec.ClusterName, config.Name)
			return config, nil
		}
		// upstream cluster is already updating, must wait until sending next update
		logrus.Infof("Waiting for cluster [%s (id: %s)] to finish updating", config.Spec.ClusterName, config.Name)
		if config.Status.Phase != aksConfigUpdatingPhase {
			config = config.DeepCopy()
			config.Status.Phase = aksConfigUpdatingPhase
			return h.aksCC.UpdateStatus(config)
		}
		h.aksEnqueueAfter(config.Namespace, config.Name, 30*time.Second)
		return config, nil
	}

	for _, np := range result.Properties.AgentPoolProfiles {
		if status := aks.String(np.ProvisioningState); status == NodePoolCreating ||
			status == NodePoolScaling || status == NodePoolDeleting || status == NodePoolUpgrading {
			// If the node pool is in an active state in Rancher but is updating in AKS, then an update was initiated outside of Rancher,
			// such as in AKS console. In this case, this is a no-op and the reconciliation will happen after syncing.
			if config.Status.Phase == aksConfigActivePhase {
				logrus.Infof("Waiting for non-Rancher initiated cluster update for [%s (id: %s)]", config.Spec.ClusterName, config.Name)
				return config, nil
			}
			switch status {
			case NodePoolDeleting:
				logrus.Infof("Waiting for cluster [%s (id: %s)] to delete node pool [%s]", config.Spec.ClusterName, config.Name, aks.String(np.Name))
			default:
				logrus.Infof("Waiting for cluster [%s (id: %s)] to update node pool [%s]", config.Spec.ClusterName, config.Name, aks.String(np.Name))
			}
			h.aksEnqueueAfter(config.Namespace, config.Name, 30*time.Second)
			return config, nil
		}
	}

	logrus.Infof("Checking configuration for cluster [%s (id: %s)]", config.Spec.ClusterName, config.Name)
	upstreamSpec, err := BuildUpstreamClusterState(ctx, h.secretsCache, h.secrets, &config.Spec)
	if err != nil {
		return config, err
	}

	return h.updateUpstreamClusterState(ctx, config, upstreamSpec)
}

func (h *Handler) validateConfig(config *aksv1.AKSClusterConfig) error {
	// Check for existing AKSClusterConfigs with the same display name
	aksConfigs, err := h.aksCC.List(config.Namespace, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("cannot list AKSClusterConfig for display name check: %w", err)
	}
	for _, c := range aksConfigs.Items {
		if c.Spec.ClusterName == config.Spec.ClusterName && c.Name != config.Name {
			return fmt.Errorf("cannot create cluster [%s (id: %s)] because an AKSClusterConfig exists with the same name", config.Spec.ClusterName, config.Name)
		}
	}

	cannotBeNilError := "field [%s] must be provided for cluster [%s (id: %s)] config"
	if config.Spec.ResourceLocation == "" {
		return fmt.Errorf(cannotBeNilError, "resourceLocation", config.Spec.ClusterName, config.Name)
	}
	if config.Spec.ResourceGroup == "" {
		return fmt.Errorf(cannotBeNilError, "resourceGroup", config.Spec.ClusterName, config.Name)
	}
	if config.Spec.ClusterName == "" {
		return fmt.Errorf(cannotBeNilError, "clusterName", config.Spec.ClusterName, config.Name)
	}
	if config.Spec.AzureCredentialSecret == "" {
		return fmt.Errorf(cannotBeNilError, "azureCredentialSecret", config.Spec.ClusterName, config.Name)
	}

	if _, err = aks.GetSecrets(h.secretsCache, h.secrets, &config.Spec); err != nil {
		return fmt.Errorf("couldn't get secret [%s] with error: %v", config.Spec.AzureCredentialSecret, err)
	}

	if config.Spec.Imported {
		return nil
	}
	if config.Spec.KubernetesVersion == nil {
		return fmt.Errorf(cannotBeNilError, "kubernetesVersion", config.Spec.ClusterName, config.Name)
	}
	if config.Spec.DNSPrefix == nil {
		return fmt.Errorf(cannotBeNilError, "dnsPrefix", config.Spec.ClusterName, config.Name)
	}

	nodeP := map[string]bool{}
	systemMode := false
	for _, np := range config.Spec.NodePools {
		if np.Name == nil {
			return fmt.Errorf(cannotBeNilError, "NodePool.Name", config.Spec.ClusterName, config.Name)
		}
		if nodeP[*np.Name] {
			return fmt.Errorf("nodePool names must be unique within the [%s (id: %s)] cluster to avoid duplication", config.Spec.ClusterName, config.Name)
		}
		nodeP[*np.Name] = true
		if np.Name == nil {
			return fmt.Errorf(cannotBeNilError, "NodePool.Name", config.Spec.ClusterName, config.Name)
		}
		if np.Count == nil {
			return fmt.Errorf(cannotBeNilError, "NodePool.Count", config.Spec.ClusterName, config.Name)
		}
		if np.MaxPods == nil {
			return fmt.Errorf(cannotBeNilError, "NodePool.MaxPods", config.Spec.ClusterName, config.Name)
		}
		if np.VMSize == "" {
			return fmt.Errorf(cannotBeNilError, "NodePool.VMSize", config.Spec.ClusterName, config.Name)
		}
		if np.OsDiskSizeGB == nil {
			return fmt.Errorf(cannotBeNilError, "NodePool.OsDiskSizeGB", config.Spec.ClusterName, config.Name)
		}
		if np.OsDiskType == "" {
			return fmt.Errorf(cannotBeNilError, "NodePool.OSDiskType", config.Spec.ClusterName, config.Name)
		}
		if np.Mode == "" {
			return fmt.Errorf(cannotBeNilError, "NodePool.Mode", config.Spec.ClusterName, config.Name)
		}
		if np.Mode == string(armcontainerservice.AgentPoolModeSystem) {
			systemMode = true
		}
		if np.OsType == "" {
			return fmt.Errorf(cannotBeNilError, "NodePool.OsType", config.Spec.ClusterName, config.Name)
		}
		if np.OsType == "Windows" {
			return fmt.Errorf("windows node pools are not currently supported")
		}
	}
	if !systemMode || len(config.Spec.NodePools) < 1 {
		return fmt.Errorf("at least one NodePool with mode System is required")
	}

	if config.Spec.NetworkPlugin != nil &&
		aks.String(config.Spec.NetworkPlugin) != string(armcontainerservice.NetworkPluginKubenet) &&
		aks.String(config.Spec.NetworkPlugin) != string(armcontainerservice.NetworkPluginAzure) {
		return fmt.Errorf("invalid network plugin value [%s] for [%s (id: %s)] cluster config", aks.String(config.Spec.NetworkPlugin), config.Spec.ClusterName, config.Name)
	}
	if config.Spec.NetworkPolicy != nil &&
		aks.String(config.Spec.NetworkPolicy) != string(armcontainerservice.NetworkPolicyAzure) &&
		aks.String(config.Spec.NetworkPolicy) != string(armcontainerservice.NetworkPolicyCalico) {
		return fmt.Errorf("invalid network policy value [%s] for [%s (id: %s)] cluster config", aks.String(config.Spec.NetworkPolicy), config.Spec.ClusterName, config.Name)
	}
	if aks.String(config.Spec.NetworkPolicy) == string(armcontainerservice.NetworkPolicyAzure) && aks.String(config.Spec.NetworkPlugin) != string(armcontainerservice.NetworkPluginAzure) {
		return fmt.Errorf("azure network policy can be used only with Azure CNI network plugin for [%s (id: %s)] cluster", config.Spec.ClusterName, config.Name)
	}

	outboundType := strings.ToLower(aks.String(config.Spec.OutboundType))
	if outboundType != "" {
		if outboundType != strings.ToLower(string(armcontainerservice.OutboundTypeLoadBalancer)) &&
			outboundType != strings.ToLower(string(armcontainerservice.OutboundTypeUserDefinedRouting)) &&
			outboundType != strings.ToLower(string(armcontainerservice.OutboundTypeManagedNATGateway)) &&
			outboundType != strings.ToLower(string(armcontainerservice.OutboundTypeUserAssignedNATGateway)) {
			return fmt.Errorf("invalid outbound type value [%s] for [%s (id: %s)] cluster config", outboundType, config.Spec.ClusterName, config.Name)
		}
		if outboundType == strings.ToLower(string(armcontainerservice.OutboundTypeUserDefinedRouting)) {
			if aks.String(config.Spec.NetworkPlugin) != string(armcontainerservice.NetworkPluginAzure) {
				return fmt.Errorf("user defined routing can be used only with Azure CNI network plugin for [%s (id: %s)] cluster", config.Spec.ClusterName, config.Name)
			}
			if config.Spec.Subnet == nil || aks.String(config.Spec.Subnet) == "" {
				return fmt.Errorf("subnet must be provided for cluster [%s (id: %s)] config when user defined routing is used", config.Spec.ClusterName, config.Name)
			}
		}
	}

	cannotBeNilErrorAzurePlugin := "field [%s] must be provided for cluster [%s (id: %s)] config when Azure CNI network plugin is used"
	if aks.String(config.Spec.NetworkPlugin) == string(armcontainerservice.NetworkPluginAzure) {
		if config.Spec.VirtualNetwork == nil {
			return fmt.Errorf(cannotBeNilErrorAzurePlugin, "virtualNetwork", config.Spec.ClusterName, config.Name)
		}
		if config.Spec.Subnet == nil {
			return fmt.Errorf(cannotBeNilErrorAzurePlugin, "subnet", config.Spec.ClusterName, config.Name)
		}
		if config.Spec.NetworkDNSServiceIP == nil {
			return fmt.Errorf(cannotBeNilErrorAzurePlugin, "dnsServiceIp", config.Spec.ClusterName, config.Name)
		}
		if config.Spec.NetworkDockerBridgeCIDR == nil {
			return fmt.Errorf(cannotBeNilErrorAzurePlugin, "dockerBridgeCidr", config.Spec.ClusterName, config.Name)
		}
		if config.Spec.NetworkServiceCIDR == nil {
			return fmt.Errorf(cannotBeNilErrorAzurePlugin, "serviceCidr", config.Spec.ClusterName, config.Name)
		}
	}
	return nil
}

func (h *Handler) waitForCluster(config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := h.azureClients.clustersClient.Get(ctx, config.Spec.ResourceGroup, config.Spec.ClusterName, nil)
	if err != nil {
		return config, err
	}

	clusterState := *result.Properties.ProvisioningState
	if clusterState == ClusterStatusFailed {
		return config, fmt.Errorf("creation for cluster [%s (id: %s)] status: %s", config.Spec.ClusterName, config.Name, clusterState)
	}
	if clusterState == ClusterStatusSucceeded {
		if err = h.createCASecret(ctx, config); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return config, err
			}
		}
		logrus.Infof("Cluster [%s (id: %s)] created successfully", config.Spec.ClusterName, config.Name)
		config = config.DeepCopy()
		config.Status.Phase = aksConfigActivePhase
		return h.aksCC.UpdateStatus(config)
	}

	logrus.Infof("Waiting for cluster [%s (id: %s)] to finish creating, cluster state: %s", config.Spec.ClusterName, config.Name, clusterState)
	h.aksEnqueueAfter(config.Namespace, config.Name, wait*time.Second)

	return config, nil
}

// enqueueUpdate enqueues the config if it is already in the updating phase. Otherwise, the
// phase is updated to "updating". This is important because the object needs to reenter the
// onChange handler to start waiting on the update.
func (h *Handler) enqueueUpdate(config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	if config.Status.Phase == aksConfigUpdatingPhase {
		h.aksEnqueue(config.Namespace, config.Name)
		return config, nil
	}
	config = config.DeepCopy()
	config.Status.Phase = aksConfigUpdatingPhase
	return h.aksCC.UpdateStatus(config)
}

// createCASecret creates a secret containing ca and endpoint. These can be used to create a kubeconfig via
// the go sdk
func (h *Handler) createCASecret(ctx context.Context, config *aksv1.AKSClusterConfig) error {
	kubeConfig, err := h.getClusterKubeConfig(ctx, &config.Spec)
	if err != nil {
		return err
	}
	endpoint := kubeConfig.Host
	ca := base64.StdEncoding.EncodeToString(kubeConfig.CAData)

	_, err = h.secrets.Create(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      config.Name,
				Namespace: config.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: aksv1.SchemeGroupVersion.String(),
						Kind:       aksClusterConfigKind,
						UID:        config.UID,
						Name:       config.Name,
					},
				},
			},
			Data: map[string][]byte{
				"endpoint": []byte(endpoint),
				"ca":       []byte(ca),
			},
		})
	return err
}

func (h *Handler) getClusterKubeConfig(ctx context.Context, spec *aksv1.AKSClusterConfigSpec) (restConfig *rest.Config, err error) {
	accessProfile, err := h.azureClients.clustersClient.GetAccessProfile(ctx, spec.ResourceGroup, spec.ClusterName, "clusterAdmin", nil)
	if err != nil {
		return nil, err
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(accessProfile.Properties.KubeConfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (h *Handler) buildUpstreamClusterState(ctx context.Context, credentials *aks.Credentials, spec *aksv1.AKSClusterConfigSpec) (*aksv1.AKSClusterConfigSpec, error) {
	upstreamSpec := &aksv1.AKSClusterConfigSpec{}

	clusterState, err := h.azureClients.clustersClient.Get(ctx, spec.ResourceGroup, spec.ClusterName, nil)
	if err != nil {
		return nil, err
	}

	// set Kubernetes version
	if clusterState.Properties.KubernetesVersion == nil {
		return nil, fmt.Errorf("cannot detect cluster [%s] upstream kubernetes version", spec.ClusterName)
	}
	upstreamSpec.KubernetesVersion = clusterState.Properties.KubernetesVersion

	// set DNS prefix
	if clusterState.Properties.DNSPrefix != nil {
		upstreamSpec.DNSPrefix = clusterState.Properties.DNSPrefix
	}

	// set tags
	upstreamSpec.Tags = make(map[string]string)
	if len(clusterState.Tags) != 0 {
		upstreamSpec.Tags = aks.StringMap(clusterState.Tags)
	}

	// set BaseURL && AuthBaseURL
	upstreamSpec.AuthBaseURL = credentials.AuthBaseURL
	upstreamSpec.BaseURL = credentials.BaseURL

	// set AgentPool profile
	for _, np := range clusterState.Properties.AgentPoolProfiles {
		var upstreamNP aksv1.AKSNodePool
		upstreamNP.Name = np.Name
		if aks.String(np.ProvisioningState) != NodePoolSucceeded || aks.Bool(np.EnableAutoScaling) {
			// If the node pool is not in a Succeeded state (i.e. it is updating or something of the like)
			// or if autoscaling is enabled, then we don't want to set the upstream node count.
			// This is because node count can vary in these two states causing continual updates to the object Spec.
			// In addition, if EnableAutoScaling is true then we don't control the number of nodes in the pool. When autoscaling changes
			// the number of nodes, then aks-operator will to try to update the node count resulting in errors that have to be manually reconciled.
			nodePoolFound := false
			for _, configNp := range spec.NodePools {
				if aks.String(configNp.Name) == aks.String(np.Name) {
					upstreamNP.Count = configNp.Count
					nodePoolFound = true
					break
				}
			}
			if !nodePoolFound {
				upstreamNP.Count = np.Count
			}
		} else {
			upstreamNP.Count = np.Count
		}
		upstreamNP.MaxPods = np.MaxPods
		upstreamNP.VMSize = *np.VMSize
		upstreamNP.OsDiskSizeGB = np.OSDiskSizeGB
		if np.OSDiskType != nil {
			upstreamNP.OsDiskType = string(*np.OSDiskType)
		}
		upstreamNP.Mode = string(*np.Mode)
		upstreamNP.OsType = string(*np.OSType)
		upstreamNP.OrchestratorVersion = np.OrchestratorVersion
		upstreamNP.AvailabilityZones = utils.ConvertToPointerOfSlice(np.AvailabilityZones)
		if np.EnableAutoScaling != nil {
			upstreamNP.EnableAutoScaling = np.EnableAutoScaling
			upstreamNP.MaxCount = np.MaxCount
			upstreamNP.MinCount = np.MinCount
		}
		upstreamNP.VnetSubnetID = np.VnetSubnetID
		upstreamNP.NodeLabels = make(map[string]*string)
		if len(np.NodeLabels) != 0 {
			upstreamNP.NodeLabels = np.NodeLabels
		}
		if np.NodeTaints != nil {
			upstreamNP.NodeTaints = utils.ConvertToPointerOfSlice(np.NodeTaints)
		}
		if np.UpgradeSettings != nil && np.UpgradeSettings.MaxSurge != nil {
			upstreamNP.MaxSurge = np.UpgradeSettings.MaxSurge
		}
		upstreamSpec.NodePools = append(upstreamSpec.NodePools, upstreamNP)
	}

	// set network configuration
	networkProfile := clusterState.Properties.NetworkProfile
	if networkProfile != nil {
		upstreamSpec.NetworkPlugin = to.Ptr(string(*networkProfile.NetworkPlugin))
		upstreamSpec.NetworkDNSServiceIP = networkProfile.DNSServiceIP
		upstreamSpec.NetworkServiceCIDR = networkProfile.ServiceCidr
		upstreamSpec.NetworkPodCIDR = networkProfile.PodCidr
		if networkProfile.NetworkPolicy != nil {
			upstreamSpec.NetworkPolicy = to.Ptr(string(*networkProfile.NetworkPolicy))
		}
		if networkProfile.OutboundType != nil {
			upstreamSpec.OutboundType = to.Ptr(string(*networkProfile.OutboundType))
		}
		if networkProfile.LoadBalancerSKU != nil {
			upstreamSpec.LoadBalancerSKU = to.Ptr(string(*networkProfile.LoadBalancerSKU))
		}
	}

	// set linux account profile
	linuxProfile := clusterState.Properties.LinuxProfile
	if linuxProfile != nil {
		upstreamSpec.LinuxAdminUsername = linuxProfile.AdminUsername
		sshKeys := linuxProfile.SSH.PublicKeys
		upstreamSpec.LinuxSSHPublicKey = sshKeys[0].KeyData
	}

	// set addons profile
	addonProfile := clusterState.Properties.AddonProfiles
	if addonProfile != nil && addonProfile["httpApplicationRouting"] != nil {
		upstreamSpec.HTTPApplicationRouting = addonProfile["httpApplicationRouting"].Enabled
	}

	// set addon monitoring profile
	if addonProfile["omsAgent"] != nil {
		upstreamSpec.Monitoring = addonProfile["omsAgent"].Enabled

		// If the monitoring addon is enabled, attempt to populate the Log Analytics
		// workspace name and group from the Azure OMS Agent configuration.
		if aks.Bool(addonProfile["omsAgent"].Enabled) && addonProfile["omsAgent"].Config != nil {
			if len(addonProfile["omsAgent"].Config) == 0 {
				return nil, fmt.Errorf("cannot set OMS Agent configuration retrieved from Azure")
			}
			logAnalyticsWorkspaceResourceID := addonProfile["omsAgent"].Config["logAnalyticsWorkspaceResourceID"]

			group := matchWorkspaceGroup.FindStringSubmatch(aks.String(logAnalyticsWorkspaceResourceID))
			if group == nil {
				return nil, fmt.Errorf("OMS Agent configuration workspace group was not found")
			}
			logAnalyticsWorkspaceGroup := group[1]
			upstreamSpec.LogAnalyticsWorkspaceGroup = to.Ptr(logAnalyticsWorkspaceGroup)

			name := matchWorkspaceName.FindStringSubmatch(aks.String(logAnalyticsWorkspaceResourceID))
			if name == nil {
				return nil, fmt.Errorf("OMS Agent configuration workspace name was not found")
			}
			logAnalyticsWorkspaceName := name[1]
			upstreamSpec.LogAnalyticsWorkspaceName = to.Ptr(logAnalyticsWorkspaceName)
		} else if addonProfile["omsAgent"].Enabled != nil && !aks.Bool(addonProfile["omsAgent"].Enabled) {
			upstreamSpec.LogAnalyticsWorkspaceGroup = nil
			upstreamSpec.LogAnalyticsWorkspaceName = nil
		}
	}

	// set API server access profile
	upstreamSpec.PrivateCluster = to.Ptr(false)
	upstreamSpec.AuthorizedIPRanges = utils.ConvertToPointerOfSlice([]*string{})
	if clusterState.Properties.APIServerAccessProfile != nil {
		if clusterState.Properties.APIServerAccessProfile.EnablePrivateCluster != nil {
			upstreamSpec.PrivateCluster = clusterState.Properties.APIServerAccessProfile.EnablePrivateCluster
		}
		if clusterState.Properties.APIServerAccessProfile.AuthorizedIPRanges != nil {
			upstreamSpec.AuthorizedIPRanges = utils.ConvertToPointerOfSlice(clusterState.Properties.APIServerAccessProfile.AuthorizedIPRanges)
		}
		if clusterState.Properties.APIServerAccessProfile.PrivateDNSZone != nil {
			upstreamSpec.PrivateDNSZone = clusterState.Properties.APIServerAccessProfile.PrivateDNSZone
		}
	}
	upstreamSpec.ManagedIdentity = to.Ptr(false)
	if clusterState.Identity != nil {
		upstreamSpec.ManagedIdentity = to.Ptr(true)
		if clusterState.Identity.UserAssignedIdentities != nil {
			for userAssignedID := range clusterState.Identity.UserAssignedIdentities {
				upstreamSpec.UserAssignedIdentity = to.Ptr(userAssignedID)
			}
		}
	}

	return upstreamSpec, err
}

// updateUpstreamClusterState compares the upstream spec with the config spec, then updates the upstream AKS cluster to
// match the config spec. Function returns after an update is finished.
func (h *Handler) updateUpstreamClusterState(ctx context.Context, config *aksv1.AKSClusterConfig, upstreamSpec *aksv1.AKSClusterConfigSpec) (*aksv1.AKSClusterConfig, error) {
	// check tags for update
	if config.Spec.Tags != nil {
		if !reflect.DeepEqual(config.Spec.Tags, upstreamSpec.Tags) {
			// If status is not updating, then enqueue the update ( to re-enter the onChange handler )
			if config.Status.Phase != aksConfigUpdatingPhase {
				return h.enqueueUpdate(config)
			}
			logrus.Infof("Updating tags for cluster [%s (id: %s)]", config.Spec.ClusterName, config.Name)
			logrus.Debugf("config: %v; upstream: %v", config.Spec.Tags, upstreamSpec.Tags)
			tags := armcontainerservice.TagsObject{
				Tags: aks.StringMapPtr(config.Spec.Tags),
			}

			poller, err := h.azureClients.clustersClient.BeginUpdateTags(ctx, config.Spec.ResourceGroup, config.Spec.ClusterName, tags, nil)
			if err != nil {
				return config, err
			}

			// Azure may have a policy that automatically adds upstream default tags to a cluster resource. We don't
			// have a good way to detect that policy. We handle this case by checking if Azure returns an unexpected
			// state for the tags and if so, log the response and move on. Any upstream tags regenerated on the cluster
			// by Azure will be synced back to rancher.
			res, err := poller.PollUntilDone(ctx, nil)
			if err != nil {
				return config, fmt.Errorf("failed to update tags for cluster [%s (id: %s)]: %w", config.Spec.ClusterName, config.Name, err)
			}

			if !reflect.DeepEqual(tags, res.Tags) {
				logrus.Infof("Tags were not updated for cluster [%s (id: %s)], config %s, upstream %s, moving on", config.Spec.ClusterName, config.Name, aks.StringMap(tags.Tags), aks.StringMap(res.Tags))
			} else {
				return h.enqueueUpdate(config)
			}
		}
	}

	// build ClusterSpec for imported clusters from upstream config,
	// otherwise ConfigSpec will be null
	importedClusterSpec := upstreamSpec.DeepCopy()
	updateAksCluster := false
	// check Kubernetes version for update
	if config.Spec.KubernetesVersion != nil {
		if aks.String(config.Spec.KubernetesVersion) != aks.String(upstreamSpec.KubernetesVersion) {
			logrus.Infof("Updating kubernetes version to %s for cluster [%s (id: %s)]", aks.String(config.Spec.KubernetesVersion), config.Spec.ClusterName, config.Name)
			logrus.Debugf("config: %s; upstream: %s", aks.String(config.Spec.KubernetesVersion), aks.String(upstreamSpec.KubernetesVersion))
			updateAksCluster = true
			importedClusterSpec.KubernetesVersion = config.Spec.KubernetesVersion
		}
	}

	// check authorized IP ranges to access AKS
	if config.Spec.AuthorizedIPRanges != nil {
		if !reflect.DeepEqual(config.Spec.AuthorizedIPRanges, upstreamSpec.AuthorizedIPRanges) {
			logrus.Infof("Updating authorized IP ranges to %v for cluster [%s (id: %s)]", config.Spec.AuthorizedIPRanges, config.Spec.ClusterName, config.Name)
			logrus.Debugf("config: %v; upstream: %v", *config.Spec.AuthorizedIPRanges, aks.StringSlice(upstreamSpec.AuthorizedIPRanges))
			updateAksCluster = true
			importedClusterSpec.AuthorizedIPRanges = config.Spec.AuthorizedIPRanges
		}
	}

	// check addon HTTP Application Routing
	if config.Spec.HTTPApplicationRouting != nil {
		if aks.Bool(config.Spec.HTTPApplicationRouting) != aks.Bool(upstreamSpec.HTTPApplicationRouting) {
			logrus.Infof("Updating HTTP application routing to %v for cluster [%s (id: %s)]", aks.Bool(config.Spec.HTTPApplicationRouting), config.Spec.ClusterName, config.Name)
			logrus.Debugf("config: %v; upstream: %v", aks.Bool(config.Spec.HTTPApplicationRouting), aks.Bool(upstreamSpec.HTTPApplicationRouting))
			updateAksCluster = true
			importedClusterSpec.HTTPApplicationRouting = config.Spec.HTTPApplicationRouting
		}
	}

	// check addon monitoring
	if config.Spec.Monitoring != nil {
		if aks.Bool(config.Spec.Monitoring) != aks.Bool(upstreamSpec.Monitoring) {
			logrus.Infof("Updating monitoring addon to %v for cluster [%s (id: %s)]", aks.Bool(config.Spec.Monitoring), config.Spec.ClusterName, config.Name)
			logrus.Debugf("[monitoring] config: %v; upstream: %v", aks.Bool(config.Spec.Monitoring), aks.Bool(upstreamSpec.Monitoring))
			logrus.Debugf("[LogAnalyticsWorkspaceGroup] config: %s; upstream: %s", aks.String(config.Spec.LogAnalyticsWorkspaceGroup), aks.String(upstreamSpec.LogAnalyticsWorkspaceGroup))
			logrus.Debugf("[LogAnalyticsWorkspaceName] config: %s; upstream: %s", aks.String(config.Spec.LogAnalyticsWorkspaceName), aks.String(upstreamSpec.LogAnalyticsWorkspaceName))
			if config.Spec.Monitoring != nil && !aks.Bool(config.Spec.Monitoring) {
				logrus.Infof("Disabling monitoring addon for cluster [%s (id: %s)]", config.Spec.ClusterName, config.Name)
				updateAksCluster = true
				importedClusterSpec.Monitoring = config.Spec.Monitoring
				importedClusterSpec.LogAnalyticsWorkspaceGroup = nil
				importedClusterSpec.LogAnalyticsWorkspaceName = nil
			} else if config.Spec.Monitoring != nil && aks.Bool(config.Spec.Monitoring) {
				logrus.Infof("Enabling monitoring addon for cluster [%s (id: %s)]", config.Spec.ClusterName, config.Name)
				updateAksCluster = true
				importedClusterSpec.Monitoring = config.Spec.Monitoring
				importedClusterSpec.LogAnalyticsWorkspaceGroup = config.Spec.LogAnalyticsWorkspaceGroup
				importedClusterSpec.LogAnalyticsWorkspaceName = config.Spec.LogAnalyticsWorkspaceName
			}
		}
	}

	if updateAksCluster {
		resourceGroupExists, err := aks.ExistsResourceGroup(ctx, h.azureClients.resourceGroupsClient, config.Spec.ResourceGroup)
		if err != nil && strings.Contains(err.Error(), "unauthorized") {
			logrus.Infof("User does not have permissions to access resource group [%s]: %s", config.Spec.ResourceGroup, err)
		}

		if !resourceGroupExists {
			logrus.Infof("Resource group [%s] does not exist, creating", config.Spec.ResourceGroup)
			if err = aks.CreateResourceGroup(ctx, h.azureClients.resourceGroupsClient, &config.Spec); err != nil {
				return config, fmt.Errorf("error during updating resource group %v", err)
			}
			logrus.Infof("Resource group [%s] updated successfully", config.Spec.ResourceGroup)
		}

		// If status is not updating, then enqueue the update (to re-enter the onChange handler)
		if config.Status.Phase != aksConfigUpdatingPhase {
			return h.enqueueUpdate(config)
		}

		upstreamNodePools, _ := utils.BuildNodePoolMap(upstreamSpec.NodePools, config.Spec.ClusterName)
		clusterSpecCopy := config.Spec.DeepCopy()
		if config.Spec.Imported {
			clusterSpecCopy = importedClusterSpec
			clusterSpecCopy.ResourceGroup = config.Spec.ResourceGroup
			clusterSpecCopy.ResourceLocation = config.Spec.ResourceLocation
			clusterSpecCopy.ClusterName = config.Spec.ClusterName
		}
		clusterSpecCopy.NodePools = make([]aksv1.AKSNodePool, 0, len(config.Spec.NodePools))
		for _, n := range config.Spec.NodePools {
			if _, ok := upstreamNodePools[*n.Name]; ok {
				clusterSpecCopy.NodePools = append(clusterSpecCopy.NodePools, n)
			}
		}
		err = aks.UpdateCluster(ctx, &h.azureClients.credentials, h.azureClients.clustersClient, h.azureClients.workplacesClient, clusterSpecCopy, config.Status.Phase)
		if err != nil {
			return config, fmt.Errorf("failed to update cluster: %v", err)
		}
		return h.enqueueUpdate(config)
	}

	if config.Spec.NodePools != nil {
		downstreamNodePools, err := utils.BuildNodePoolMap(config.Spec.NodePools, config.Spec.ClusterName)
		if err != nil {
			return config, err
		}

		// check for updated NodePools
		upstreamNodePools, _ := utils.BuildNodePoolMap(upstreamSpec.NodePools, config.Spec.ClusterName)
		for npName, np := range downstreamNodePools {
			updateNodePool := false
			upstreamNodePool, ok := upstreamNodePools[npName]
			if ok {
				if upstreamNodePool.VnetSubnetID != nil {
					np.VnetSubnetID = upstreamNodePool.VnetSubnetID
				}

				if aks.Bool(np.EnableAutoScaling) {
					// Count can't be updated when EnableAutoScaling is true, so don't send anything.
					np.Count = nil
					if !aks.Bool(upstreamNodePool.EnableAutoScaling) {
						logrus.Infof("Enable autoscaling in node pool [%s] for cluster [%s (id: %s)]", aks.String(np.Name), config.Spec.ClusterName, config.Name)
						updateNodePool = true
					}
					if aks.Int32(np.MinCount) != aks.Int32(upstreamNodePool.MinCount) {
						logrus.Infof("Updating minimum count to %d in node pool [%s] for cluster [%s (id: %s)]", aks.Int32(np.MinCount), aks.String(np.Name), config.Spec.ClusterName, config.Name)
						logrus.Debugf("config: %d; upstream: %d", aks.Int32(np.MinCount), aks.Int32(upstreamNodePool.MinCount))
						updateNodePool = true
					}
					if aks.Int32(np.MaxCount) != aks.Int32(upstreamNodePool.MaxCount) {
						logrus.Infof("Updating maximum count to %d in node pool [%s] for cluster [%s (id: %s)]", aks.Int32(np.MaxCount), aks.String(np.Name), config.Spec.ClusterName, config.Name)
						logrus.Debugf("config: %d; upstream: %d", aks.Int32(np.MaxPods), aks.Int32(upstreamNodePool.MaxCount))
						updateNodePool = true
					}
				} else {
					if np.MinCount != nil && np.MaxCount != nil {
						return config, fmt.Errorf("min and max node count must be nil for node pool [%s] for cluster [%s (id: %s)], because autoscaling is disabled", aks.String(np.Name), config.Spec.ClusterName, config.Name)
					}
					if aks.Bool(upstreamNodePool.EnableAutoScaling) {
						logrus.Infof("Disable autoscaling in node pool [%s] for cluster [%s (id: %s)]", aks.String(np.Name), config.Spec.ClusterName, config.Name)
						updateNodePool = true
					} else if aks.Int32(np.Count) != aks.Int32(upstreamNodePool.Count) {
						logrus.Infof("Updating node count to %d in node pool [%s] for cluster [%s (id: %s)]", aks.Int32(np.Count), aks.String(np.Name), config.Spec.ClusterName, config.Name)
						logrus.Debugf("config: %d; upstream: %d", aks.Int32(np.Count), aks.Int32(upstreamNodePool.Count))
						updateNodePool = true
					}
				}
				if np.OrchestratorVersion != nil && aks.String(np.OrchestratorVersion) != aks.String(upstreamNodePool.OrchestratorVersion) {
					logrus.Infof("Updating orchestrator version to %s in node pool [%s] for cluster [%s (id: %s)]", aks.String(np.OrchestratorVersion), aks.String(np.Name), config.Spec.ClusterName, config.Name)
					logrus.Debugf("config: %s; upstream: %s", aks.String(np.OrchestratorVersion), aks.String(upstreamNodePool.OrchestratorVersion))
					updateNodePool = true
				}
				if np.NodeLabels != nil {
					if !reflect.DeepEqual(np.NodeLabels, upstreamNodePool.NodeLabels) {
						logrus.Infof("Updating labels to %v in node pool [%s] for cluster [%s (id: %s)]", aks.StringMap(np.NodeLabels), aks.String(np.Name), config.Spec.ClusterName, config.Name)
						logrus.Debugf("config: %v; upstream: %v", aks.StringMap(np.NodeLabels), aks.StringMap(upstreamNodePool.NodeLabels))
						updateNodePool = true
					}
				}
				if np.NodeTaints != nil {
					if !(len(*np.NodeTaints) == 0 && upstreamNodePool.NodeTaints == nil) {
						if !reflect.DeepEqual(np.NodeTaints, upstreamNodePool.NodeTaints) {
							logrus.Infof("Updating node taints to %v in node pool [%s] for cluster [%s (id: %s)]", aks.StringSlice(np.NodeTaints), aks.String(np.Name), config.Spec.ClusterName, config.Name)
							logrus.Debugf("config: %v; upstream: %v", aks.StringSlice(np.NodeTaints), aks.StringSlice(upstreamNodePool.NodeTaints))
							updateNodePool = true
						}
					}
				}
				if np.MaxSurge != nil && aks.String(np.MaxSurge) != aks.String(upstreamNodePool.MaxSurge) {
					logrus.Infof("Updating max surge to %s in node pool [%s] for cluster [%s (id: %s)]", aks.String(np.MaxSurge), aks.String(np.Name), config.Spec.ClusterName, config.Name)
					logrus.Debugf("config: %s; upstream: %s", aks.String(np.MaxSurge), aks.String(upstreamNodePool.MaxSurge))
					updateNodePool = true
				}
				if np.Mode != "" && np.Mode != upstreamNodePool.Mode {
					logrus.Infof("Updating mode to %s in node pool [%s] for cluster [%s (id: %s)]", np.Mode, aks.String(np.Name), config.Spec.ClusterName, config.Name)
					logrus.Debugf("config: %s; upstream: %s", np.Mode, upstreamNodePool.Mode)
					updateNodePool = true
				}
				if np.AvailabilityZones != nil && upstreamNodePool.AvailabilityZones != nil && !availabilityZonesEqual(*np.AvailabilityZones, *upstreamNodePool.AvailabilityZones) {
					return config, fmt.Errorf("changing availability zones for node pool [%s] in cluster [%s (id: %s)] is not permitted", aks.String(np.Name), config.Spec.ClusterName, config.Name)
				}
			} else {
				logrus.Infof("Adding node pool [%s] for cluster [%s (id: %s)]", aks.String(np.Name), config.Spec.ClusterName, config.Name)
				updateNodePool = true
			}

			if updateNodePool {
				// If status is not updating, then enqueue the update ( to re-enter the onChange handler )
				if config.Status.Phase != aksConfigUpdatingPhase {
					return h.enqueueUpdate(config)
				}
				err = aks.CreateOrUpdateAgentPool(ctx, h.azureClients.agentPoolsClient, &config.Spec, np)
				if err != nil {
					return config, fmt.Errorf("failed to update cluster [%s (id: %s)]: %v", config.Spec.ClusterName, config.Name, err)
				}
				return h.enqueueUpdate(config)
			}
		}

		// check for removed NodePools
		for npName := range upstreamNodePools {
			if _, ok := downstreamNodePools[npName]; !ok {
				if upstreamNodePools[npName].Mode == string(armcontainerservice.AgentPoolModeSystem) {
					// at least one NodePool with mode System is required in the cluster
					systemNodePoolExists := false
					for _, tmpNodePool := range downstreamNodePools {
						if tmpNodePool.Mode == string(armcontainerservice.AgentPoolModeSystem) {
							systemNodePoolExists = true
						}
					}
					if !systemNodePoolExists {
						return config, fmt.Errorf("cannot remove node pool [%s] with mode System from cluster [%s (id: %s)]", npName, config.Spec.ClusterName, config.Name)
					}
				}
				// If status is not updating, then enqueue the update ( to re-enter the onChange handler )
				if config.Status.Phase != aksConfigUpdatingPhase {
					return h.enqueueUpdate(config)
				}
				logrus.Infof("Removing node pool [%s] from cluster [%s (id: %s)]", npName, config.Spec.ClusterName, config.Name)
				err = aks.RemoveAgentPool(ctx, h.azureClients.agentPoolsClient, &config.Spec, upstreamNodePools[npName])
				if err != nil {
					return config, fmt.Errorf("failed to remove node pool: %v", err)
				}
				return h.enqueueUpdate(config)
			}
		}
	}

	// no new updates, set to active
	if config.Status.Phase != aksConfigActivePhase {
		logrus.Infof("Cluster [%s (id: %s)] finished updating", config.Spec.ClusterName, config.Name)
		config = config.DeepCopy()
		config.Status.Phase = aksConfigActivePhase
		return h.aksCC.UpdateStatus(config)
	}

	logrus.Infof("Configuration for cluster [%s (id: %s)] was verified", config.Spec.ClusterName, config.Name)
	return config, nil
}

func (h *Handler) getAzureClients(config *aksv1.AKSClusterConfig) error {
	credentials, err := aks.GetSecrets(h.secretsCache, h.secrets, &config.Spec)
	if err != nil {
		return fmt.Errorf("error getting credentials: %w", err)
	}

	clientSecretCredential, err := aks.NewClientSecretCredential(credentials)
	if err != nil {
		return fmt.Errorf("error creating client secret credential: %w", err)
	}

	clustersClient, err := services.NewManagedClustersClient(credentials.SubscriptionID, clientSecretCredential, credentials.Cloud)
	if err != nil {
		return fmt.Errorf("error creating managed cluster client: %w", err)
	}
	rgClient, err := services.NewResourceGroupsClient(credentials.SubscriptionID, clientSecretCredential, credentials.Cloud)
	if err != nil {
		return fmt.Errorf("error creating resource group client: %w", err)
	}
	agentPoolsClient, err := services.NewAgentPoolClient(credentials.SubscriptionID, clientSecretCredential, credentials.Cloud)
	if err != nil {
		return fmt.Errorf("error creating agent pool client: %w", err)
	}
	workplacesClient, err := services.NewWorkplacesClient(credentials.SubscriptionID, clientSecretCredential, credentials.Cloud)
	if err != nil {
		return fmt.Errorf("error creating workplace client: %w", err)
	}

	h.azureClients = azureClients{
		credentials:          *credentials,
		clustersClient:       clustersClient,
		resourceGroupsClient: rgClient,
		agentPoolsClient:     agentPoolsClient,
		workplacesClient:     workplacesClient,
	}

	return nil
}

func availabilityZonesEqual(a, b []string) bool {
	// If lengths don't match, they're not equal
	if len(a) != len(b) {
		return false
	}

	// Compare each element
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
