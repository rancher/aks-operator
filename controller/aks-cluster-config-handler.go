package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	containersv209 "github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-09-01/containerservice"
	containersv211 "github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/Azure/azure-sdk-for-go/services/resources/mgmt/2019-10-01/resources"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/to"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	v10 "github.com/rancher/aks-operator/pkg/generated/controllers/aks.cattle.io/v1"
	wranglerv1 "github.com/rancher/wrangler/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v15 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	controllerName           = "aks-controller"
	controllerRemoveName     = "aks-controller-remove"
	aksConfigNotCreatedPhase = ""
	aksConfigActivePhase     = "active"
	aksConfigUpdatingPhase   = "updating"
	aksConfigImportingPhase  = "importing"
	aksClusterConfigKind     = "AKSClusterConfig"
)

type Handler struct {
	aksCC           v10.AKSClusterConfigClient
	aksEnqueueAfter func(namespace, name string, duration time.Duration)
	aksEnqueue      func(namespace, name string)
	secrets         wranglerv1.SecretClient
	secretsCache    wranglerv1.SecretCache
}

func Register(
	ctx context.Context,
	secrets wranglerv1.SecretController,
	aks v10.AKSClusterConfigController) {

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

func (h *Handler) OnAksConfigChanged(key string, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	if config == nil || config.DeletionTimestamp != nil {
		return nil, nil
	}

	ctx := context.Background()
	switch config.Status.Phase {
	case aksConfigImportingPhase:
		return h.importCluster(ctx, config)
	case aksConfigNotCreatedPhase:
		return h.createCluster(ctx, config)
	case aksConfigActivePhase, aksConfigUpdatingPhase:
		return h.checkCluster(ctx, config)
	default:
		return config, fmt.Errorf("invalid phase: %v", config.Status.Phase)
	}
}

func (h *Handler) OnAksConfigRemoved(key string, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	if config.Spec.Imported {
		logrus.Infof("Cluster [%s] is imported, will not delete AKS cluster", config.Spec.ClusterName)
		return config, nil
	}

	ctx := context.Background()
	logrus.Infof("Removing cluster [%s]", config.Spec.ClusterName)

	azureAuthorizer, err := newClientAuthorizer(h.secretsCache, config.Spec)
	if err != nil {
		return config, err
	}
	resourceGroupsClient, err := newResourceGroupsClient(azureAuthorizer, config.Spec)
	if err != nil {
		return config, err
	}
	resourceClusterClient, err := newClustersClient(azureAuthorizer, config.Spec)
	if err != nil {
		return config, err
	}

	if existsCluster(ctx, resourceClusterClient, config.Spec) {
		if err := removeCluster(ctx, resourceClusterClient, config.Spec); err != nil {
			return config, fmt.Errorf("error removing cluster [%s] message %v", config.Spec.ClusterName, err)
		}
	}

	logrus.Infof("Removing resource group [%s] for cluster [%s]", config.Spec.ResourceGroup, config.Spec.ClusterName)

	if existsResourceGroup(ctx, resourceGroupsClient, config.Spec.ResourceGroup) {
		if err := removeResourceGroup(ctx, resourceGroupsClient, config.Spec); err != nil {
			logrus.Errorf("Error removing resource group [%s] message: %v", config.Spec.ResourceGroup, err)
			return config, err
		}
	}

	logrus.Infof("Cluster [%s] was removed successfully", config.Spec.ClusterName)
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

func (h *Handler) createCluster(ctx context.Context, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
	if err := h.validateConfig(ctx, config); err != nil {
		return config, err
	}

	if config.Spec.Imported {
		config = config.DeepCopy()
		config.Status.Phase = aksConfigImportingPhase
		return h.aksCC.UpdateStatus(config)
	}

	logrus.Infof("Creating cluster [%s]", config.Spec.ClusterName)

	azureAuthorizer, err := newClientAuthorizer(h.secretsCache, config.Spec)
	if err != nil {
		return config, err
	}
	resourceGroupsClient, err := newResourceGroupsClient(azureAuthorizer, config.Spec)
	if err != nil {
		return config, err
	}

	logrus.Infof("Checking if resource group [%s] exists", config.Spec.ResourceGroup)

	if !existsResourceGroup(ctx, resourceGroupsClient, config.Spec.ResourceGroup) {
		logrus.Infof("Creating resource group [%s] for cluster [%s]", config.Spec.ResourceGroup, config.Spec.ClusterName)
		err := createResourceGroup(ctx, resourceGroupsClient, config.Spec)
		if err != nil {
			return config, fmt.Errorf("error creating resource group [%s] with message %v", config.Spec.ResourceGroup, err)
		}
		logrus.Infof("Resource group [%s] created successfully", config.Spec.ResourceGroup)
	}

	logrus.Infof("Creating AKS cluster [%s]", config.Spec.ClusterName)
	resourceClusterClient, err := newClustersClient(azureAuthorizer, config.Spec)
	if err != nil {
		return config, err
	}
	result, err := createOrUpdateCluster(ctx, h.secretsCache, resourceClusterClient, config.Spec)
	if err != nil {
		return config, fmt.Errorf("error failed to create cluster: %v ", err)
	}

	clusterState := *result.ManagedClusterProperties.ProvisioningState
	if clusterState != "Succeeded" {
		return config, fmt.Errorf("error during provisioning cluster [%s] with status %v", config.Spec.ClusterName, clusterState)
	}

	logrus.Infof("Cluster [%s] created successfully", config.Spec.ClusterName)

	if err := h.createCASecret(ctx, config); err != nil {
		if !errors.IsAlreadyExists(err) {
			return config, err
		}
	}
	config = config.DeepCopy()
	config.Status.Phase = aksConfigActivePhase
	return h.aksCC.UpdateStatus(config)
}

func (h *Handler) importCluster(ctx context.Context, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {

	logrus.Infof("Importing config for cluster [%s]", config.Spec.ClusterName)

	if err := h.createCASecret(ctx, config); err != nil {
		if !errors.IsAlreadyExists(err) {
			return config, err
		}
	}

	config = config.DeepCopy()
	config.Status.Phase = aksConfigActivePhase
	return h.aksCC.UpdateStatus(config)
}

func (h *Handler) checkCluster(ctx context.Context, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {

	logrus.Infof("Checking if cluster [%s] exists", config.Spec.ClusterName)
	upstreamSpec, err := BuildUpstreamClusterState(ctx, h.secretsCache, config.Spec)
	if err != nil {
		return config, err
	}
	_, err = updateUpstreamClusterState(ctx, h.secretsCache, config.Spec, upstreamSpec)
	if err != nil {
		return config, err
	}

	logrus.Infof("Status for cluster [%s] was checked", config.Spec.ClusterName)

	if config.Status.Phase == aksConfigUpdatingPhase {
		config = config.DeepCopy()
		config.Status.Phase = aksConfigActivePhase
		return h.aksCC.UpdateStatus(config)
	}

	return config, nil
}

func (h *Handler) validateConfig(ctx context.Context, config *aksv1.AKSClusterConfig) error {
	// Check for existing AKSClusterConfigs with the same display name
	aksConfigs, err := h.aksCC.List(config.Namespace, v15.ListOptions{})
	if err != nil {
		return fmt.Errorf("cannot list AKSClusterConfig for display name check")
	}
	for _, c := range aksConfigs.Items {
		if c.Spec.ClusterName == config.Spec.ClusterName && c.Name != config.Name  {
			return fmt.Errorf("cannot create cluster [%s] because an AKSClusterConfig exists with the same name", config.Spec.ClusterName)
		}
	}

	cannotBeNilError := "field [%s] must be provided for cluster [%s] config"
	if config.Spec.ResourceLocation == "" {
		return fmt.Errorf(cannotBeNilError, "resourceLocation", config.ClusterName)
	}
	if config.Spec.ResourceGroup == "" {
		return fmt.Errorf(cannotBeNilError, "resourceGroup", config.ClusterName)
	}
	if config.Spec.ClusterName == "" {
		return fmt.Errorf(cannotBeNilError, "clusterName", config.ClusterName)
	}
	if config.Spec.SubscriptionID == "" {
		return fmt.Errorf(cannotBeNilError, "subscriptionId", config.ClusterName)
	}
	if config.Spec.TenantID == "" {
		return fmt.Errorf(cannotBeNilError, "tenantId", config.ClusterName)
	}
	if config.Spec.AzureCredentialSecret == "" {
		return fmt.Errorf(cannotBeNilError, "azureCredentialSecret", config.ClusterName)
	}
	clientId, clientSecret, err := getSecrets(h.secretsCache, config.Spec.AzureCredentialSecret)
	if err != nil {
		return fmt.Errorf("could not get secrets with error: %v", err)
	}
	if clientId == "" || clientSecret == "" {
		return fmt.Errorf(cannotBeNilError, "clientId and clientSecret", config.ClusterName)
	}
	if config.Spec.Imported {
		return nil
	}
	if config.Spec.KubernetesVersion == nil {
		return fmt.Errorf(cannotBeNilError, "kubernetesVersion", config.ClusterName)
	}

	azureAuthorizer, err := newClientAuthorizer(h.secretsCache, config.Spec)
	if err != nil {
		return err
	}
	serviceClient, err := newServiceClient(azureAuthorizer, config.Spec)
	if err != nil {
		return err
	}
	listOperators, err := serviceClient.ListOrchestrators(ctx, config.Spec.ResourceLocation, "managedClusters")
	if err != nil {
		return err
	}
	objOrchestrators := *listOperators.Orchestrators
	validKubernetesVersion := false
	for _, obj := range objOrchestrators {
		if to.String(config.Spec.KubernetesVersion) == to.String(obj.OrchestratorVersion) {
			validKubernetesVersion = true
			break
		}
	}
	if !validKubernetesVersion {
		return fmt.Errorf("field kubernetesVersion is invalid for non-import [%s] cluster config", config.ClusterName)
	}
	if hasAgentPoolProfile(config.Spec) {
		for _, np := range config.Spec.NodePools {
			if np.Name == nil {
				return fmt.Errorf(cannotBeNilError, "NodePool.Name", config.ClusterName)
			}
			if np.Count == nil {
				return fmt.Errorf(cannotBeNilError, "NodePool.Count", config.ClusterName)
			}
			if np.MaxPods == nil {
				return fmt.Errorf(cannotBeNilError, "NodePool.MaxPods", config.ClusterName)
			}
			if np.VMSize == "" {
				return fmt.Errorf(cannotBeNilError, "NodePool.VMSize", config.ClusterName)
			}
			if np.OsDiskSizeGB == nil {
				return fmt.Errorf(cannotBeNilError, "NodePool.OsDiskSizeGB", config.ClusterName)
			}
			if np.OsDiskType == "" {
				return fmt.Errorf(cannotBeNilError, "NodePool.OSDiskType", config.ClusterName)
			}
			if np.Mode == "" {
				return fmt.Errorf(cannotBeNilError, "NodePool.Mode", config.ClusterName)
			}
			if np.OsType == "" {
				return fmt.Errorf(cannotBeNilError, "NodePool.OsType", config.ClusterName)
			}
		}
	}

	if config.Spec.NetworkPolicy != nil &&
		*config.Spec.NetworkPolicy != string(containersv211.NetworkPolicyAzure) &&
		*config.Spec.NetworkPolicy != string(containersv211.NetworkPolicyCalico) {
		return fmt.Errorf("wrong network policy value for [%s] cluster config", config.ClusterName)
	}
	return nil
}

// createCASecret creates a secret containing ca and endpoint. These can be used to create a kubeconfig via
// the go sdk
func (h *Handler) createCASecret(ctx context.Context, config *aksv1.AKSClusterConfig) error {
	kubeConfig, err := GetClusterKubeConfig(ctx, h.secretsCache, config.Spec)
	if err != nil {
		return err
	}
	endpoint := kubeConfig.Host
	ca := string(kubeConfig.CAData)

	_, err = h.secrets.Create(
		&v1.Secret{
			ObjectMeta: v15.ObjectMeta{
				Name:      config.Name,
				Namespace: config.Namespace,
				OwnerReferences: []v15.OwnerReference{
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

func GetClusterKubeConfig(ctx context.Context, secretsCache wranglerv1.SecretCache, spec aksv1.AKSClusterConfigSpec) (restConfig *rest.Config, err error) {
	azureAuthorizer, err := newClientAuthorizer(secretsCache, spec)
	if err != nil {
		return nil, err
	}
	resourceClusterClient, err := newClustersClient(azureAuthorizer, spec)
	if err != nil {
		return nil, err
	}
	accessProfile, err := resourceClusterClient.GetAccessProfile(ctx, spec.ResourceGroup, spec.ClusterName, "clusterAdmin")
	if err != nil {
		return nil, err
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(*accessProfile.KubeConfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

// BuildUpstreamClusterState creates AKSClusterConfigSpec from existing cluster configuration
func BuildUpstreamClusterState(ctx context.Context, secretsCache wranglerv1.SecretCache, spec aksv1.AKSClusterConfigSpec) (*aksv1.AKSClusterConfigSpec, error) {
	upstreamSpec := &aksv1.AKSClusterConfigSpec{}

	upstreamSpec.Imported = true

	azureAuthorizer, err := newClientAuthorizer(secretsCache, spec)
	if err != nil {
		return nil, err
	}
	resourceClusterClient, err := newClustersClient(azureAuthorizer, spec)
	if err != nil {
		return nil, err
	}
	clusterState, err := resourceClusterClient.Get(ctx, spec.ResourceGroup, spec.ClusterName)
	if err != nil {
		return nil, err
	}

	// set Kubernetes version
	if clusterState.KubernetesVersion == nil {
		return nil, fmt.Errorf("cannot detect cluster [%s] upstream kubernetes version", spec.ClusterName)
	}
	upstreamSpec.KubernetesVersion = clusterState.KubernetesVersion

	// set tags
	upstreamSpec.Tags = make(map[string]string)
	if len(clusterState.Tags) != 0 {
		upstreamSpec.Tags = to.StringMap(clusterState.Tags)
	}

	// set AgentPool profile
	for _, np := range *clusterState.AgentPoolProfiles {
		var upstreamNP aksv1.AKSNodePool
		upstreamNP.Name = np.Name
		upstreamNP.Count = np.Count
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
		upstreamSpec.NodePools = append(upstreamSpec.NodePools, upstreamNP)
	}

	// set network configuration
	networkProfile := clusterState.NetworkProfile
	if networkProfile != nil {
		upstreamSpec.NetworkPlugin = to.StringPtr(string(networkProfile.NetworkPlugin))
		upstreamSpec.NetworkDNSServiceIP = networkProfile.DNSServiceIP
		upstreamSpec.NetworkDockerBridgeCIDR = networkProfile.DockerBridgeCidr
		upstreamSpec.NetworkServiceCIDR = networkProfile.ServiceCidr
		upstreamSpec.NetworkPolicy = to.StringPtr(string(networkProfile.NetworkPolicy))
		upstreamSpec.NetworkPodCIDR = networkProfile.PodCidr
		upstreamSpec.LoadBalancerSKU = to.StringPtr(string(networkProfile.LoadBalancerSku))
	}

	// set linux account profile
	linuxProfile := clusterState.LinuxProfile
	if linuxProfile != nil {
		upstreamSpec.LinuxAdminUsername = linuxProfile.AdminUsername
		sshKeys := *linuxProfile.SSH.PublicKeys
		upstreamSpec.LinuxSSHPublicKey = sshKeys[0].KeyData
	}

	// set API server access profile
	upstreamSpec.PrivateCluster = to.BoolPtr(false)
	if clusterState.APIServerAccessProfile != nil {
		if clusterState.APIServerAccessProfile.EnablePrivateCluster != nil {
			upstreamSpec.PrivateCluster = clusterState.APIServerAccessProfile.EnablePrivateCluster
		}
		if clusterState.APIServerAccessProfile.AuthorizedIPRanges != nil {
			upstreamSpec.AuthorizedIPRanges = clusterState.APIServerAccessProfile.AuthorizedIPRanges
		}
	}

	return upstreamSpec, err
}

// updateUpstreamClusterState compares the upstream spec with the config spec, then updates the upstream AKS cluster to
// match the config spec. Function returns after a update is finished.
func updateUpstreamClusterState(ctx context.Context, secretsCache wranglerv1.SecretCache, spec aksv1.AKSClusterConfigSpec, upstreamSpec *aksv1.AKSClusterConfigSpec) (*aksv1.AKSClusterConfigSpec, error) {
	if spec.Tags == nil {
		spec.Tags = map[string]string{}
	}
	spec.Tags["displayName"] = spec.ClusterName
	if spec.Imported || reflect.DeepEqual(spec.Tags, upstreamSpec.Tags) &&
		spec.NodePools != nil && reflect.DeepEqual(spec.NodePools, upstreamSpec.NodePools) {
		return nil, nil
	}

	azureAuthorizer, err := newClientAuthorizer(secretsCache, spec)
	if err != nil {
		return nil, err
	}
	resourceGroupsClient, err := newResourceGroupsClient(azureAuthorizer, spec)
	if err != nil {
		return nil, err
	}

	if !existsResourceGroup(ctx, resourceGroupsClient, spec.ResourceGroup) {
		logrus.Infof("Resource group [%s] does not exist, creating", spec.ResourceGroup)
		if err := createResourceGroup(ctx, resourceGroupsClient, spec); err != nil {
			return nil, fmt.Errorf("error during updating resource group %v", err)
		}
		logrus.Infof("Resource group [%s] updated successfully", spec.ResourceGroup)
	}

	resourceClusterClient, err := newClustersClient(azureAuthorizer, spec)

	result, err := createOrUpdateCluster(ctx, secretsCache, resourceClusterClient, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to update cluster: %v", err)
	}

	if result.ManagedClusterProperties.ProvisioningState == nil ||
		*result.ManagedClusterProperties.ProvisioningState == "Succeeded" {
		logrus.Infof("Cluster [%s] was updated successfully", spec.ClusterName)
		return nil, nil
	}
	return nil, fmt.Errorf("cluster [%s] was updated with error: %s", spec.ClusterName, *result.ManagedClusterProperties.ProvisioningState)
}

func newClientAuthorizer(secretsCache wranglerv1.SecretCache, spec aksv1.AKSClusterConfigSpec) (autorest.Authorizer, error) {
	authBaseURL := spec.AuthBaseURL
	if authBaseURL != nil {
		authBaseURL = &azure.PublicCloud.ActiveDirectoryEndpoint
	}
	if spec.TenantID == "" {
		return nil, fmt.Errorf("tenantId not specified")
	}

	oauthConfig, err := adal.NewOAuthConfig(*authBaseURL, spec.TenantID)
	if err != nil {
		return nil, err
	}

	baseURL := spec.BaseURL
	if baseURL != nil {
		baseURL = &azure.PublicCloud.ResourceManagerEndpoint
	}

	clientId, clientSecret, err := getSecrets(secretsCache, spec.AzureCredentialSecret)
	if err != nil {
		return nil, fmt.Errorf("couldn't get secrets with error: %v", err)
	}
	spToken, err := adal.NewServicePrincipalToken(*oauthConfig, clientId, clientSecret, *baseURL)
	if err != nil {
		return nil, fmt.Errorf("couldn't authenticate to Azure cloud with error: %v", err)
	}

	return autorest.NewBearerAuthorizer(spToken), nil
}

func newResourceGroupsClient(authorizer autorest.Authorizer, spec aksv1.AKSClusterConfigSpec) (*resources.GroupsClient, error) {
	if authorizer == nil {
		return nil, fmt.Errorf("authorizer must not be nil")
	}

	baseURL := spec.BaseURL
	if baseURL != nil {
		baseURL = &azure.PublicCloud.ResourceManagerEndpoint
	}

	client := resources.NewGroupsClientWithBaseURI(*baseURL, spec.SubscriptionID)
	client.Authorizer = authorizer

	return &client, nil
}

func createResourceGroup(ctx context.Context, groupsClient *resources.GroupsClient, spec aksv1.AKSClusterConfigSpec) error {
	_, err := groupsClient.CreateOrUpdate(
		ctx,
		spec.ResourceGroup,
		resources.Group{
			Name:     to.StringPtr(spec.ResourceGroup),
			Location: to.StringPtr(spec.ResourceLocation),
		})

	return err
}

func existsResourceGroup(ctx context.Context, groupsClient *resources.GroupsClient, resourceGroup string) bool {
	resp, err := groupsClient.CheckExistence(ctx, resourceGroup)
	if err != nil {
		return false
	}
	if resp.StatusCode == 204 {
		return true
	}

	return false
}

func removeResourceGroup(ctx context.Context, groupsClient *resources.GroupsClient, spec aksv1.AKSClusterConfigSpec) error {
	if !existsResourceGroup(ctx, groupsClient, spec.ResourceGroup) {
		logrus.Infof("Resource group %s for cluster [%s] doesn't exist", spec.ResourceGroup, spec.ClusterName)
		return nil
	}

	future, err := groupsClient.Delete(ctx, spec.ResourceGroup)
	if err != nil {
		return fmt.Errorf("error removing resource group '%s': %v", spec.ResourceGroup, err)
	}

	if err = future.WaitForCompletionRef(ctx, groupsClient.Client); err != nil {
		return fmt.Errorf("cannot get the AKS cluster create or update future response: %v", err)
	}

	logrus.Infof("Resource group %s for cluster [%s] removed successfully", spec.ResourceGroup, spec.ClusterName)
	return nil
}

func newClustersClient(authorizer autorest.Authorizer, spec aksv1.AKSClusterConfigSpec) (*containersv211.ManagedClustersClient, error) {
	if authorizer == nil {
		return nil, fmt.Errorf("authorizer must not be nil")
	}

	baseURL := spec.BaseURL
	if baseURL != nil {
		baseURL = &azure.PublicCloud.ResourceManagerEndpoint
	}

	client := containersv211.NewManagedClustersClientWithBaseURI(*baseURL, spec.SubscriptionID)
	client.Authorizer = authorizer

	return &client, nil
}

func newServiceClient(authorizer autorest.Authorizer, spec aksv1.AKSClusterConfigSpec) (*containersv209.ContainerServicesClient, error) {
	if authorizer == nil {
		return nil, fmt.Errorf("authorizer must not be nil")
	}

	baseURL := spec.BaseURL
	if baseURL != nil {
		baseURL = &azure.PublicCloud.ResourceManagerEndpoint
	}

	client := containersv209.NewContainerServicesClientWithBaseURI(*baseURL, spec.SubscriptionID)
	client.Authorizer = authorizer

	return &client, nil
}

// createOrUpdateCluster creates a new managed Kubernetes cluster
func createOrUpdateCluster(ctx context.Context, secretsCache wranglerv1.SecretCache, clusterClient *containersv211.ManagedClustersClient, spec aksv1.AKSClusterConfigSpec) (c containersv211.ManagedCluster, err error) {

	dnsPrefix := spec.DNSPrefix
	if dnsPrefix == nil {
		dnsPrefix = to.StringPtr(spec.ClusterName)
	}

	tags := make(map[string]*string)
	for key, val := range spec.Tags {
		if val != "" {
			tags[key] = to.StringPtr(val)
		}
	}
	displayName := spec.ClusterName
	if displayName == "" {
		displayName = spec.ClusterName
	}
	tags["displayName"] = to.StringPtr(displayName)

	kubernetesVersion := spec.KubernetesVersion

	var vmNetSubnetID *string
	networkProfile := &containersv211.NetworkProfile{}
	if hasCustomVirtualNetwork(spec) {
		virtualNetworkResourceGroup := spec.ResourceGroup

		//if virtual network resource group is set, use it, otherwise assume it is the same as the cluster
		if spec.VirtualNetworkResourceGroup != nil {
			virtualNetworkResourceGroup = *spec.VirtualNetworkResourceGroup
		}

		vmNetSubnetID = to.StringPtr(fmt.Sprintf(
			"/subscriptions/%v/resourceGroups/%v/providers/Microsoft.Network/virtualNetworks/%v/subnets/%v",
			spec.SubscriptionID,
			virtualNetworkResourceGroup,
			spec.VirtualNetwork,
			spec.Subnet,
		))

		networkProfile.DNSServiceIP = spec.NetworkDNSServiceIP
		networkProfile.DockerBridgeCidr = spec.NetworkDockerBridgeCIDR
		networkProfile.ServiceCidr = spec.NetworkServiceCIDR

		if spec.NetworkPlugin != nil {
			networkProfile.NetworkPlugin = containersv211.Kubenet
		} else {
			networkProfile.NetworkPlugin = containersv211.NetworkPlugin(*spec.NetworkPlugin)
		}

		// if network plugin is 'kubenet', set PodCIDR
		if networkProfile.NetworkPlugin == containersv211.Azure {
			networkProfile.PodCidr = spec.NetworkPodCIDR
		}

		if spec.LoadBalancerSKU != nil {
			loadBalancerSku := containersv211.LoadBalancerSku(*spec.LoadBalancerSKU)
			networkProfile.LoadBalancerSku = loadBalancerSku
		}

		if spec.NetworkPolicy != nil {
			networkProfile.NetworkPolicy = containersv211.NetworkPolicy(*spec.NetworkPolicy)
		}
	}

	var agentPoolProfiles []containersv211.ManagedClusterAgentPoolProfile
	if hasAgentPoolProfile(spec) {
		for _, np := range spec.NodePools {
			if np.OrchestratorVersion == nil {
				np.OrchestratorVersion = spec.KubernetesVersion
			}
			agentProfile := containersv211.ManagedClusterAgentPoolProfile{
				Name:                np.Name,
				Count:               np.Count,
				MaxPods:             np.MaxPods,
				OsDiskSizeGB:        np.OsDiskSizeGB,
				OsType:              containersv211.OSType(np.OsType),
				VMSize:              containersv211.VMSizeTypes(np.VMSize),
				Mode:                containersv211.AgentPoolMode(np.Mode),
				OrchestratorVersion: np.OrchestratorVersion,
			}
			if np.AvailabilityZones != nil {
				agentProfile.AvailabilityZones = np.AvailabilityZones
			}
			if np.EnableAutoScaling != nil && *np.EnableAutoScaling {
				agentProfile.EnableAutoScaling = np.EnableAutoScaling
				agentProfile.MaxCount = np.MaxCount
				agentProfile.MinCount = np.MinCount
			}
			if hasCustomVirtualNetwork(spec) {
				agentProfile.VnetSubnetID = vmNetSubnetID
			}
			agentPoolProfiles = append(agentPoolProfiles, agentProfile)
		}
	}

	var linuxProfile *containersv211.LinuxProfile
	if hasLinuxProfile(spec) {
		linuxProfile = &containersv211.LinuxProfile{
			AdminUsername: spec.LinuxAdminUsername,
			SSH: &containersv211.SSHConfiguration{
				PublicKeys: &[]containersv211.SSHPublicKey{
					{
						KeyData: spec.LinuxSSHPublicKey,
					},
				},
			},
		}
	}

	clientId, clientSecret, err := getSecrets(secretsCache, spec.AzureCredentialSecret)
	if err != nil {
		return c, fmt.Errorf("couldn't get secrets with error: %v", err)
	}
	managedCluster := containersv211.ManagedCluster{
		Name:     to.StringPtr(spec.ClusterName),
		Location: to.StringPtr(spec.ResourceLocation),
		Tags:     tags,
		ManagedClusterProperties: &containersv211.ManagedClusterProperties{
			KubernetesVersion: kubernetesVersion,
			DNSPrefix:         dnsPrefix,
			AgentPoolProfiles: &agentPoolProfiles,
			LinuxProfile:      linuxProfile,
			NetworkProfile:    networkProfile,
			ServicePrincipalProfile: &containersv211.ManagedClusterServicePrincipalProfile{
				ClientID: to.StringPtr(clientId),
				Secret:   to.StringPtr(clientSecret),
			},
		},
	}

	if spec.AuthorizedIPRanges != nil {
		managedCluster.APIServerAccessProfile = &containersv211.ManagedClusterAPIServerAccessProfile{
			AuthorizedIPRanges: spec.AuthorizedIPRanges,
		}
	}
	if spec.PrivateCluster != nil && *spec.PrivateCluster {
		managedCluster.APIServerAccessProfile = &containersv211.ManagedClusterAPIServerAccessProfile{
			EnablePrivateCluster: spec.PrivateCluster,
		}
	}

	future, err := clusterClient.CreateOrUpdate(
		ctx,
		spec.ResourceGroup,
		spec.ClusterName,
		managedCluster,
	)
	if err != nil {
		return c, fmt.Errorf("[AKS] cannot create AKS cluster: %v", err)
	}

	if err = future.WaitForCompletionRef(ctx, clusterClient.Client); err != nil {
		return c, fmt.Errorf("can't get the AKS cluster create or update future response: %v", err)
	}

	return future.Result(*clusterClient)
}

// Check if AKS managed Kubernetes cluster exist
func existsCluster(ctx context.Context, clusterClient *containersv211.ManagedClustersClient, spec aksv1.AKSClusterConfigSpec) bool {
	resp, err := clusterClient.Get(ctx, spec.ResourceGroup, spec.ClusterName)

	return err == nil && resp.StatusCode == 200
}

// Delete AKS managed Kubernetes cluster
func removeCluster(ctx context.Context, clusterClient *containersv211.ManagedClustersClient, spec aksv1.AKSClusterConfigSpec) (err error) {
	future, err := clusterClient.Delete(ctx, spec.ResourceGroup, spec.ClusterName)
	if err != nil {
		return err
	}

	err = future.WaitForCompletionRef(ctx, clusterClient.Client)
	if err != nil {
		logrus.Errorf("can't get the AKS cluster create or update future response: %v", err)
		return err
	}

	logrus.Infof("Cluster %v removed successfully", spec.ClusterName)
	logrus.Infof("Cluster removal status %v", future.Status())

	return nil
}

func hasCustomVirtualNetwork(spec aksv1.AKSClusterConfigSpec) bool {
	return spec.VirtualNetwork != nil && spec.Subnet != nil
}

func hasAgentPoolProfile(spec aksv1.AKSClusterConfigSpec) bool {
	return len(spec.NodePools) > 0
}

func hasLinuxProfile(spec aksv1.AKSClusterConfigSpec) bool {
	return spec.LinuxAdminUsername != nil && spec.LinuxSSHPublicKey != nil
}

func getSecrets(secretsCache wranglerv1.SecretCache, secretName string) (string, string, error) {
	if secretName == "" {
		return "", "", fmt.Errorf("secret name not provided")
	}

	ns, id := ParseSecretName(secretName)
	if ns == "" {
		ns = "default"
	}
	secret, err := secretsCache.Get(ns, id)
	if err != nil {
		return "", "", fmt.Errorf("couldn't find secret [%s] in namespace [%s]", id, ns)
	}

	clientIdBytes := secret.Data["clientId"]
	clientSecretBytes := secret.Data["clientSecret"]
	if clientIdBytes == nil || clientSecretBytes == nil {
		return "", "", fmt.Errorf("invalid secret client data for Azure cloud")
	}

	return string(clientIdBytes), string(clientSecretBytes), nil
}
