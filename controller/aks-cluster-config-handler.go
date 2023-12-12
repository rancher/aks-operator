package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/rancher/aks-operator/pkg/aks"
	"github.com/rancher/aks-operator/pkg/aks/services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	akscontrollers "github.com/rancher/aks-operator/pkg/generated/controllers/aks.cattle.io/v1"
	"github.com/rancher/aks-operator/pkg/utils"
	wranglerv1 "github.com/rancher/wrangler/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
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
		logrus.Infof("Cluster [%s] is imported, will not delete AKS cluster", config.Spec.ClusterName)
		return config, nil
	}
	if config.Status.Phase == aksConfigNotCreatedPhase {
		// The most likely context here is that the cluster already existed in AKS, so we shouldn't delete it
		logrus.Warnf("Cluster [%s] never advanced to creating status, will not delete AKS cluster", config.Name)
		return config, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logrus.Infof("Removing cluster [%s]", config.Spec.ClusterName)

	clusterExists, err := aks.ExistsCluster(ctx, h.azureClients.clustersClient, &config.Spec)
	if err != nil && strings.Contains(err.Error(), "unauthorized") {
		logrus.Infof("user does not have permissions to access cluster [%s]: %s", config.Spec.ClusterName, err)
	}

	if clusterExists {
		if err = aks.RemoveCluster(ctx, h.azureClients.clustersClient, &config.Spec); err != nil {
			return config, fmt.Errorf("error removing cluster [%s] message %v", config.Spec.ClusterName, err)
		}
	}

	logrus.Infof("Cluster [%s] was removed successfully", config.Spec.ClusterName)
	logrus.Infof("Resource group [%s] for cluster [%s] still exists, please remove it if needed", config.Spec.ResourceGroup, config.Spec.ClusterName)

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
			logrus.Errorf("Error recording akscc [%s] failure message: %s", config.Name, recordErr.Error())
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

	logrus.Infof("Creating cluster [%s]", config.Spec.ClusterName)

	logrus.Infof("Checking if cluster [%s] exists", config.Spec.ClusterName)

	clusterExists, err := aks.ExistsCluster(ctx, h.azureClients.clustersClient, &config.Spec)
	if err != nil && strings.Contains(err.Error(), "unauthorized") {
		logrus.Infof("user does not have permissions to access cluster [%s]: %s", config.Spec.ClusterName, err)
	}

	if clusterExists {
		return config, fmt.Errorf("cluster [%s] already exists in AKS. Update configuration or import the existing one", config.Spec.ClusterName)
	}

	logrus.Infof("Checking if resource group [%s] exists", config.Spec.ResourceGroup)

	resourceGroupExists, err := aks.ExistsResourceGroup(ctx, h.azureClients.resourceGroupsClient, config.Spec.ResourceGroup)
	if err != nil && strings.Contains(err.Error(), "unauthorized") {
		logrus.Infof("user does not have permissions to access resource group [%s]: %s", config.Spec.ResourceGroup, err)
	}

	if !resourceGroupExists {
		logrus.Infof("Creating resource group [%s] for cluster [%s]", config.Spec.ResourceGroup, config.Spec.ClusterName)
		err = aks.CreateResourceGroup(ctx, h.azureClients.resourceGroupsClient, &config.Spec)
		if err != nil {
			return config, fmt.Errorf("error creating resource group [%s] with message %v", config.Spec.ResourceGroup, err)
		}
		logrus.Infof("Resource group [%s] created successfully", config.Spec.ResourceGroup)
	}

	logrus.Infof("Creating AKS cluster [%s]", config.Spec.ClusterName)

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

	logrus.Infof("Importing config for cluster [%s]", config.Spec.ClusterName)

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

	result, err := h.azureClients.clustersClient.Get(ctx, config.Spec.ResourceGroup, config.Spec.ClusterName)
	if err != nil {
		return config, err
	}

	if config.Status.RBACEnabled == nil && result.EnableRBAC != nil {
		config = config.DeepCopy()
		config.Status.RBACEnabled = result.EnableRBAC
		return h.aksCC.UpdateStatus(config)
	}

	clusterState := *result.ManagedClusterProperties.ProvisioningState
	if clusterState == ClusterStatusFailed {
		return config, fmt.Errorf("update failed for cluster [%s], status: %s", config.Spec.ClusterName, clusterState)
	}
	if clusterState == ClusterStatusInProgress || clusterState == ClusterStatusUpdating {
		// If the cluster is in an active state in Rancher but is updating in AKS, then an update was initiated outside of Rancher,
		// such as in AKS console. In this case, this is a no-op and the reconciliation will happen after syncing.
		if config.Status.Phase == aksConfigActivePhase {
			logrus.Infof("Waiting for non-Rancher initiated cluster update for [%s]", config.Name)
			return config, nil
		}
		// upstream cluster is already updating, must wait until sending next update
		logrus.Infof("Waiting for cluster [%s] to finish updating", config.Name)
		if config.Status.Phase != aksConfigUpdatingPhase {
			config = config.DeepCopy()
			config.Status.Phase = aksConfigUpdatingPhase
			return h.aksCC.UpdateStatus(config)
		}
		h.aksEnqueueAfter(config.Namespace, config.Name, 30*time.Second)
		return config, nil
	}

	for _, np := range *result.AgentPoolProfiles {
		if status := to.String(np.ProvisioningState); status == NodePoolCreating ||
			status == NodePoolScaling || status == NodePoolDeleting || status == NodePoolUpgrading {
			// If the node pool is in an active state in Rancher but is updating in AKS, then an update was initiated outside of Rancher,
			// such as in AKS console. In this case, this is a no-op and the reconciliation will happen after syncing.
			if config.Status.Phase == aksConfigActivePhase {
				logrus.Infof("Waiting for non-Rancher initiated cluster update for [%s]", config.Name)
				return config, nil
			}
			switch status {
			case NodePoolDeleting:
				logrus.Infof("Waiting for cluster [%s] to delete node pool [%s]", config.Name, to.String(np.Name))
			default:
				logrus.Infof("Waiting for cluster [%s] to update node pool [%s]", config.Name, to.String(np.Name))
			}
			h.aksEnqueueAfter(config.Namespace, config.Name, 30*time.Second)
			return config, nil
		}
	}

	logrus.Infof("Checking configuration for cluster [%s]", config.Spec.ClusterName)
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
			return fmt.Errorf("cannot create cluster [%s] because an AKSClusterConfig exists with the same name", config.Spec.ClusterName)
		}
	}

	cannotBeNilError := "field [%s] must be provided for cluster [%s] config"
	if config.Spec.ResourceLocation == "" {
		return fmt.Errorf(cannotBeNilError, "resourceLocation", config.Spec.ClusterName)
	}
	if config.Spec.ResourceGroup == "" {
		return fmt.Errorf(cannotBeNilError, "resourceGroup", config.Spec.ClusterName)
	}
	if config.Spec.ClusterName == "" {
		return fmt.Errorf(cannotBeNilError, "clusterName", config.Spec.ClusterName)
	}
	if config.Spec.AzureCredentialSecret == "" {
		return fmt.Errorf(cannotBeNilError, "azureCredentialSecret", config.Spec.ClusterName)
	}

	if _, err = aks.GetSecrets(h.secretsCache, h.secrets, &config.Spec); err != nil {
		return fmt.Errorf("couldn't get secret [%s] with error: %v", config.Spec.AzureCredentialSecret, err)
	}

	if config.Spec.Imported {
		return nil
	}
	if config.Spec.KubernetesVersion == nil {
		return fmt.Errorf(cannotBeNilError, "kubernetesVersion", config.Spec.ClusterName)
	}
	if config.Spec.DNSPrefix == nil {
		return fmt.Errorf(cannotBeNilError, "dnsPrefix", config.Spec.ClusterName)
	}

	nodeP := map[string]bool{}
	systemMode := false
	for _, np := range config.Spec.NodePools {
		if np.Name == nil {
			return fmt.Errorf(cannotBeNilError, "NodePool.Name", config.Spec.ClusterName)
		}
		if nodeP[*np.Name] {
			return fmt.Errorf("NodePool names must be unique within the [%s] cluster to avoid duplication", config.Spec.ClusterName)
		}
		nodeP[*np.Name] = true
		if np.Name == nil {
			return fmt.Errorf(cannotBeNilError, "NodePool.Name", config.Spec.ClusterName)
		}
		if np.Count == nil {
			return fmt.Errorf(cannotBeNilError, "NodePool.Count", config.Spec.ClusterName)
		}
		if np.MaxPods == nil {
			return fmt.Errorf(cannotBeNilError, "NodePool.MaxPods", config.Spec.ClusterName)
		}
		if np.VMSize == "" {
			return fmt.Errorf(cannotBeNilError, "NodePool.VMSize", config.Spec.ClusterName)
		}
		if np.OsDiskSizeGB == nil {
			return fmt.Errorf(cannotBeNilError, "NodePool.OsDiskSizeGB", config.Spec.ClusterName)
		}
		if np.OsDiskType == "" {
			return fmt.Errorf(cannotBeNilError, "NodePool.OSDiskType", config.Spec.ClusterName)
		}
		if np.Mode == "" {
			return fmt.Errorf(cannotBeNilError, "NodePool.Mode", config.Spec.ClusterName)
		}
		if np.Mode == "System" {
			systemMode = true
		}
		if np.OsType == "" {
			return fmt.Errorf(cannotBeNilError, "NodePool.OsType", config.Spec.ClusterName)
		}
		if np.OsType == "Windows" {
			return fmt.Errorf("windows node pools are not currently supported")
		}
	}
	if !systemMode || len(config.Spec.NodePools) < 1 {
		return fmt.Errorf("at least one NodePool with mode System is required")
	}

	if config.Spec.NetworkPlugin != nil &&
		to.String(config.Spec.NetworkPlugin) != string(containerservice.Kubenet) &&
		to.String(config.Spec.NetworkPlugin) != string(containerservice.Azure) {
		return fmt.Errorf("invalid network plugin value [%s] for [%s] cluster config", to.String(config.Spec.NetworkPlugin), config.Spec.ClusterName)
	}
	if config.Spec.NetworkPolicy != nil &&
		to.String(config.Spec.NetworkPolicy) != string(containerservice.NetworkPolicyAzure) &&
		to.String(config.Spec.NetworkPolicy) != string(containerservice.NetworkPolicyCalico) {
		return fmt.Errorf("invalid network policy value [%s] for [%s] cluster config", to.String(config.Spec.NetworkPolicy), config.Spec.ClusterName)
	}
	if to.String(config.Spec.NetworkPolicy) == string(containerservice.NetworkPolicyAzure) && to.String(config.Spec.NetworkPlugin) != string(containerservice.Azure) {
		return fmt.Errorf("azure network policy can be used only with Azure CNI network plugin for [%s] cluster", config.Spec.ClusterName)
	}
	cannotBeNilErrorAzurePlugin := "field [%s] must be provided for cluster [%s] config when Azure CNI network plugin is used"
	if to.String(config.Spec.NetworkPlugin) == string(containerservice.Azure) {
		if config.Spec.VirtualNetwork == nil {
			return fmt.Errorf(cannotBeNilErrorAzurePlugin, "virtualNetwork", config.Spec.ClusterName)
		}
		if config.Spec.Subnet == nil {
			return fmt.Errorf(cannotBeNilErrorAzurePlugin, "subnet", config.Spec.ClusterName)
		}
		if config.Spec.NetworkDNSServiceIP == nil {
			return fmt.Errorf(cannotBeNilErrorAzurePlugin, "dnsServiceIp", config.Spec.ClusterName)
		}
		if config.Spec.NetworkServiceCIDR == nil {
			return fmt.Errorf(cannotBeNilErrorAzurePlugin, "serviceCidr", config.Spec.ClusterName)
		}
	}
	return nil
}

func (h *Handler) waitForCluster(config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := h.azureClients.clustersClient.Get(ctx, config.Spec.ResourceGroup, config.Spec.ClusterName)
	if err != nil {
		return config, err
	}

	clusterState := *result.ManagedClusterProperties.ProvisioningState
	if clusterState == ClusterStatusFailed {
		return config, fmt.Errorf("creation for cluster [%s] status: %s", config.Spec.ClusterName, clusterState)
	}
	if clusterState == ClusterStatusSucceeded {
		if err = h.createCASecret(ctx, config); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return config, err
			}
		}
		logrus.Infof("Cluster [%s] created successfully", config.Spec.ClusterName)
		config = config.DeepCopy()
		config.Status.Phase = aksConfigActivePhase
		return h.aksCC.UpdateStatus(config)
	}

	logrus.Infof("Waiting for cluster [%s] to finish creating, cluster state: %s", config.Name, clusterState)
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
	accessProfile, err := h.azureClients.clustersClient.GetAccessProfile(ctx, spec.ResourceGroup, spec.ClusterName, "clusterAdmin")
	if err != nil {
		return nil, err
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(*accessProfile.KubeConfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (h *Handler) buildUpstreamClusterState(ctx context.Context, credentials *aks.Credentials, spec *aksv1.AKSClusterConfigSpec) (*aksv1.AKSClusterConfigSpec, error) {
	upstreamSpec := &aksv1.AKSClusterConfigSpec{}

	clusterState, err := h.azureClients.clustersClient.Get(ctx, spec.ResourceGroup, spec.ClusterName)
	if err != nil {
		return nil, err
	}

	// set Kubernetes version
	if clusterState.KubernetesVersion == nil {
		return nil, fmt.Errorf("cannot detect cluster [%s] upstream kubernetes version", spec.ClusterName)
	}
	upstreamSpec.KubernetesVersion = clusterState.KubernetesVersion

	// set DNS prefix
	if clusterState.DNSPrefix != nil {
		upstreamSpec.DNSPrefix = clusterState.DNSPrefix
	}

	// set tags
	upstreamSpec.Tags = make(map[string]string)
	if len(clusterState.Tags) != 0 {
		upstreamSpec.Tags = to.StringMap(clusterState.Tags)
	}

	// set BaseURL && AuthBaseURL
	upstreamSpec.AuthBaseURL = credentials.AuthBaseURL
	upstreamSpec.BaseURL = credentials.BaseURL

	// set AgentPool profile
	for _, np := range *clusterState.AgentPoolProfiles {
		var upstreamNP aksv1.AKSNodePool
		upstreamNP.Name = np.Name
		if to.String(np.ProvisioningState) != NodePoolSucceeded || to.Bool(np.EnableAutoScaling) {
			// If the node pool is not in a Succeeded state (i.e. it is updating or something of the like)
			// or if autoscaling is enabled, then we don't want to set the upstream node count.
			// This is because node count can vary in these two states causing continual updates to the object Spec.
			// In addition, if EnableAutoScaling is true then we don't control the number of nodes in the pool. When autoscaling changes
			// the number of nodes, then aks-operator will to try to update the node count resulting in errors that have to be manually reconciled.
			nodePoolFound := false
			for _, configNp := range spec.NodePools {
				if to.String(configNp.Name) == to.String(np.Name) {
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
		upstreamNP.VMSize = string(np.VMSize)
		upstreamNP.OsDiskSizeGB = np.OsDiskSizeGB
		upstreamNP.OsDiskType = string(np.OsDiskType)
		upstreamNP.Mode = string(np.Mode)
		upstreamNP.OsType = string(np.OsType)
		upstreamNP.OrchestratorVersion = np.OrchestratorVersion
		upstreamNP.AvailabilityZones = np.AvailabilityZones
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
			upstreamNP.NodeTaints = np.NodeTaints
		}
		if np.UpgradeSettings != nil && np.UpgradeSettings.MaxSurge != nil {
			upstreamNP.MaxSurge = np.UpgradeSettings.MaxSurge
		}
		upstreamSpec.NodePools = append(upstreamSpec.NodePools, upstreamNP)
	}

	// set network configuration
	networkProfile := clusterState.NetworkProfile
	if networkProfile != nil {
		upstreamSpec.NetworkPlugin = to.StringPtr(string(networkProfile.NetworkPlugin))
		upstreamSpec.NetworkDNSServiceIP = networkProfile.DNSServiceIP
		upstreamSpec.NetworkServiceCIDR = networkProfile.ServiceCidr
		upstreamSpec.NetworkPolicy = to.StringPtr(string(networkProfile.NetworkPolicy))
		upstreamSpec.NetworkPodCIDR = networkProfile.PodCidr
		upstreamSpec.OutboundType = to.StringPtr(string(networkProfile.OutboundType))
		upstreamSpec.LoadBalancerSKU = to.StringPtr(string(networkProfile.LoadBalancerSku))
	}

	// set linux account profile
	linuxProfile := clusterState.LinuxProfile
	if linuxProfile != nil {
		upstreamSpec.LinuxAdminUsername = linuxProfile.AdminUsername
		sshKeys := *linuxProfile.SSH.PublicKeys
		upstreamSpec.LinuxSSHPublicKey = sshKeys[0].KeyData
	}

	// set addons profile
	addonProfile := clusterState.AddonProfiles
	if addonProfile != nil && addonProfile["httpApplicationRouting"] != nil {
		upstreamSpec.HTTPApplicationRouting = addonProfile["httpApplicationRouting"].Enabled
	}

	// set addon monitoring profile
	if addonProfile["omsAgent"] != nil {
		upstreamSpec.Monitoring = addonProfile["omsAgent"].Enabled

		if len(addonProfile["omsAgent"].Config) == 0 {
			return nil, fmt.Errorf("cannot set OMS Agent configuration retrieved from Azure")
		}
		logAnalyticsWorkspaceResourceID := addonProfile["omsAgent"].Config["logAnalyticsWorkspaceResourceID"]

		group := matchWorkspaceGroup.FindStringSubmatch(to.String(logAnalyticsWorkspaceResourceID))
		if group == nil {
			return nil, fmt.Errorf("OMS Agent configuration workspace group was not found")
		}
		logAnalyticsWorkspaceGroup := group[1]
		upstreamSpec.LogAnalyticsWorkspaceGroup = to.StringPtr(logAnalyticsWorkspaceGroup)

		name := matchWorkspaceName.FindStringSubmatch(to.String(logAnalyticsWorkspaceResourceID))
		if name == nil {
			return nil, fmt.Errorf("OMS Agent configuration workspace name was not found")
		}
		logAnalyticsWorkspaceName := name[1]
		upstreamSpec.LogAnalyticsWorkspaceName = to.StringPtr(logAnalyticsWorkspaceName)
	}

	// set API server access profile
	upstreamSpec.PrivateCluster = to.BoolPtr(false)
	if clusterState.APIServerAccessProfile != nil {
		if clusterState.APIServerAccessProfile.EnablePrivateCluster != nil {
			upstreamSpec.PrivateCluster = clusterState.APIServerAccessProfile.EnablePrivateCluster
		}
		if clusterState.APIServerAccessProfile.AuthorizedIPRanges != nil && *clusterState.APIServerAccessProfile.AuthorizedIPRanges != nil {
			upstreamSpec.AuthorizedIPRanges = clusterState.APIServerAccessProfile.AuthorizedIPRanges
		}
		if clusterState.APIServerAccessProfile.PrivateDNSZone != nil {
			upstreamSpec.PrivateDNSZone = clusterState.APIServerAccessProfile.PrivateDNSZone
		}
	}
	upstreamSpec.ManagedIdentity = to.BoolPtr(false)
	if clusterState.Identity != nil {
		upstreamSpec.ManagedIdentity = to.BoolPtr(true)
		if clusterState.Identity.UserAssignedIdentities != nil {
			for userAssignedID := range clusterState.Identity.UserAssignedIdentities {
				upstreamSpec.UserAssignedIdentity = to.StringPtr(userAssignedID)
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
			logrus.Infof("Updating tags for cluster [%s]", config.Spec.ClusterName)
			tags := containerservice.TagsObject{
				Tags: *to.StringMapPtr(config.Spec.Tags),
			}
			response, err := h.azureClients.clustersClient.UpdateTags(ctx, config.Spec.ResourceGroup, config.Spec.ClusterName, tags)
			if err != nil {
				return config, err
			}

			// Azure may have a policy that automatically adds upstream default tags to a cluster resource. We don't
			// have a good way to detect that policy. We handle this case by checking if Azure returns an unexpected
			// state for the tags and if so, log the response and move on. Any upstream tags regenerated on the cluster
			// by Azure will be synced back to rancher.
			upstreamTags := containerservice.TagsObject{}
			if err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
				return strings.HasSuffix(err.Error(), "asynchronous operation has not completed")
			}, func() error {
				managedCluster, err := h.azureClients.clustersClient.AsyncUpdateTagsResult(response)
				upstreamTags.Tags = managedCluster.Tags
				return err
			}); err != nil {
				return config, fmt.Errorf("failed to update tags for cluster [%s]: %w", config.Spec.ClusterName, err)
			}

			if !reflect.DeepEqual(tags, upstreamTags) && response.Response().StatusCode == http.StatusOK {
				logrus.Infof("Tags were not updated as expected for cluster [%s], expected %s, actual %s, moving on", config.Spec.ClusterName, to.StringMap(tags.Tags), to.StringMap(upstreamTags.Tags))
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
		if to.String(config.Spec.KubernetesVersion) != to.String(upstreamSpec.KubernetesVersion) {
			logrus.Infof("Updating kubernetes version for cluster [%s]", config.Spec.ClusterName)
			updateAksCluster = true
			importedClusterSpec.KubernetesVersion = config.Spec.KubernetesVersion
		}
	}

	// check authorized IP ranges to access AKS
	if config.Spec.AuthorizedIPRanges != nil {
		if !reflect.DeepEqual(config.Spec.AuthorizedIPRanges, upstreamSpec.AuthorizedIPRanges) {
			logrus.Infof("Updating authorized IP ranges for cluster [%s]", config.Spec.ClusterName)
			updateAksCluster = true
			importedClusterSpec.AuthorizedIPRanges = config.Spec.AuthorizedIPRanges
		}
	}

	// check addon HTTP Application Routing
	if config.Spec.HTTPApplicationRouting != nil {
		if to.Bool(config.Spec.HTTPApplicationRouting) != to.Bool(upstreamSpec.HTTPApplicationRouting) {
			logrus.Infof("Updating HTTP application routing for cluster [%s]", config.Spec.ClusterName)
			updateAksCluster = true
			importedClusterSpec.HTTPApplicationRouting = config.Spec.HTTPApplicationRouting
		}
	}

	// check addon monitoring
	if config.Spec.Monitoring != nil {
		if to.Bool(config.Spec.Monitoring) != to.Bool(upstreamSpec.Monitoring) {
			logrus.Infof("Updating monitoring addon for cluster [%s]", config.Spec.ClusterName)
			updateAksCluster = true
			importedClusterSpec.Monitoring = config.Spec.Monitoring
			importedClusterSpec.LogAnalyticsWorkspaceGroup = config.Spec.LogAnalyticsWorkspaceGroup
			importedClusterSpec.LogAnalyticsWorkspaceName = config.Spec.LogAnalyticsWorkspaceName
		}
	}

	if updateAksCluster {
		resourceGroupExists, err := aks.ExistsResourceGroup(ctx, h.azureClients.resourceGroupsClient, config.Spec.ResourceGroup)
		if err != nil && strings.Contains(err.Error(), "unauthorized") {
			logrus.Infof("user does not have permissions to access resource group [%s]: %s", config.Spec.ResourceGroup, err)
		}

		if !resourceGroupExists {
			logrus.Infof("Resource group [%s] does not exist, creating", config.Spec.ResourceGroup)
			if err = aks.CreateResourceGroup(ctx, h.azureClients.resourceGroupsClient, &config.Spec); err != nil {
				return config, fmt.Errorf("error during updating resource group %v", err)
			}
			logrus.Infof("Resource group [%s] updated successfully", config.Spec.ResourceGroup)
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

				if to.Bool(np.EnableAutoScaling) {
					// Count can't be updated when EnableAutoScaling is true, so don't send anything.
					np.Count = nil
					if !to.Bool(upstreamNodePool.EnableAutoScaling) {
						logrus.Infof("Enable autoscaling in node pool [%s] for cluster [%s]", to.String(np.Name), config.Spec.ClusterName)
						updateNodePool = true
					}
					if to.Int32(np.MinCount) != to.Int32(upstreamNodePool.MinCount) {
						logrus.Infof("Updating minimum count in node pool [%s] for cluster [%s]", to.String(np.Name), config.Spec.ClusterName)
						updateNodePool = true
					}
					if to.Int32(np.MaxCount) != to.Int32(upstreamNodePool.MaxCount) {
						logrus.Infof("Updating maximum count in node pool [%s] for cluster [%s]", to.String(np.Name), config.Spec.ClusterName)
						updateNodePool = true
					}
				} else {
					if np.MinCount != nil && np.MaxCount != nil {
						return config, fmt.Errorf("min and max node count must be nil for node pool [%s] for cluster [%s], because autoscaling is disabled", to.String(np.Name), config.Spec.ClusterName)
					}
					if to.Bool(upstreamNodePool.EnableAutoScaling) {
						logrus.Infof("Disable autoscaling in node pool [%s] for cluster [%s]", to.String(np.Name), config.Spec.ClusterName)
						updateNodePool = true
					} else if to.Int32(np.Count) != to.Int32(upstreamNodePool.Count) {
						logrus.Infof("Updating node count in node pool [%s] for cluster [%s]", to.String(np.Name), config.Spec.ClusterName)
						updateNodePool = true
					}
				}
				if np.OrchestratorVersion != nil && to.String(np.OrchestratorVersion) != to.String(upstreamNodePool.OrchestratorVersion) {
					logrus.Infof("Updating orchestrator version in node pool [%s] for cluster [%s]", to.String(np.Name), config.Spec.ClusterName)
					updateNodePool = true
				}
				if np.NodeLabels != nil {
					if !reflect.DeepEqual(np.NodeLabels, upstreamNodePool.NodeLabels) {
						logrus.Infof("Updating labels in node pool [%s] for cluster [%s]", to.String(np.Name), config.Spec.ClusterName)
						updateNodePool = true
					}
				}
				if np.NodeTaints != nil {
					if !(len(*np.NodeTaints) == 0 && upstreamNodePool.NodeTaints == nil) {
						if !reflect.DeepEqual(np.NodeTaints, upstreamNodePool.NodeTaints) {
							logrus.Infof("Updating node taints in node pool [%s] for cluster [%s]", to.String(np.Name), config.Spec.ClusterName)
							updateNodePool = true
						}
					}
				}
				if np.MaxSurge != nil && to.String(np.MaxSurge) != to.String(upstreamNodePool.MaxSurge) {
					logrus.Infof("Updating max surge in node pool [%s] for cluster [%s]", to.String(np.Name), config.Spec.ClusterName)
					updateNodePool = true
				}
				if np.Mode != "" && np.Mode != upstreamNodePool.Mode {
					logrus.Infof("Updating mode in node pool [%s] for cluster [%s]", to.String(np.Name), config.Spec.ClusterName)
					updateNodePool = true
				}
			} else {
				logrus.Infof("Adding node pool [%s] for cluster [%s]", to.String(np.Name), config.Spec.ClusterName)
				updateNodePool = true
			}

			if updateNodePool {
				err = aks.CreateOrUpdateAgentPool(ctx, h.azureClients.agentPoolsClient, &config.Spec, np)
				if err != nil {
					return config, fmt.Errorf("failed to update cluster: %v", err)
				}
				return h.enqueueUpdate(config)
			}
		}

		// check for removed NodePools
		for npName := range upstreamNodePools {
			if _, ok := downstreamNodePools[npName]; !ok {
				logrus.Infof("Removing node pool [%s] from cluster [%s]", npName, config.Spec.ClusterName)
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
		logrus.Infof("Cluster [%s] finished updating", config.Name)
		config = config.DeepCopy()
		config.Status.Phase = aksConfigActivePhase
		return h.aksCC.UpdateStatus(config)
	}

	logrus.Infof("Configuration for cluster [%s] was verified", config.Spec.ClusterName)
	return config, nil
}

func (h *Handler) getAzureClients(config *aksv1.AKSClusterConfig) error {
	credentials, err := aks.GetSecrets(h.secretsCache, h.secrets, &config.Spec)
	if err != nil {
		return fmt.Errorf("error getting credentials: %w", err)
	}

	authorizer, err := aks.NewClientAuthorizer(credentials)
	if err != nil {
		return fmt.Errorf("error creating authorizer: %w", err)
	}

	clustersClient, err := services.NewManagedClustersClient(authorizer, *credentials.BaseURL, credentials.SubscriptionID)
	if err != nil {
		return fmt.Errorf("error creating managed cluster client: %w", err)
	}
	rgClient, err := services.NewResourceGroupsClient(authorizer, *credentials.BaseURL, credentials.SubscriptionID)
	if err != nil {
		return fmt.Errorf("error creating resource group client: %w", err)
	}
	agentPoolsClient, err := services.NewAgentPoolClient(authorizer, *credentials.BaseURL, credentials.SubscriptionID)
	if err != nil {
		return fmt.Errorf("error creating agent pool client: %w", err)
	}
	workplacesClient, err := services.NewWorkplacesClient(authorizer, *credentials.BaseURL, credentials.SubscriptionID)
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
