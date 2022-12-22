package aks

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/Azure/azure-sdk-for-go/services/resources/mgmt/2019-10-01/resources"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/rancher/aks-operator/pkg/aks/services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
)

func CreateResourceGroup(ctx context.Context, groupsClient services.ResourceGroupsClientInterface, spec *aksv1.AKSClusterConfigSpec) error {
	_, err := groupsClient.CreateOrUpdate(
		ctx,
		spec.ResourceGroup,
		resources.Group{
			Name:     to.StringPtr(spec.ResourceGroup),
			Location: to.StringPtr(spec.ResourceLocation),
		},
	)
	return err
}

// CreateCluster creates a new managed Kubernetes cluster. In this case, there will be no existing upstream cluster.
// We are provisioning a brand new one.
func CreateCluster(ctx context.Context, cred *Credentials, clusterClient services.ManagedClustersClientInterface, workplaceClient services.WorkplacesClientInterface,
	spec *aksv1.AKSClusterConfigSpec, phase string) error {

	managedCluster, err := newManagedCluster(ctx, cred, workplaceClient, spec, phase)
	if err != nil {
		return err
	}

	_, err = clusterClient.CreateOrUpdate(
		ctx,
		spec.ResourceGroup,
		spec.ClusterName,
		*managedCluster,
	)

	return err
}

// newManagedCluster creates a new managed Kubernetes cluster.
func newManagedCluster(ctx context.Context, cred *Credentials, workplacesClient services.WorkplacesClientInterface, spec *aksv1.AKSClusterConfigSpec, phase string) (*containerservice.ManagedCluster, error) {
	managedCluster := &containerservice.ManagedCluster{
		Name:     to.StringPtr(spec.ClusterName),
		Location: to.StringPtr(spec.ResourceLocation),
		ManagedClusterProperties: &containerservice.ManagedClusterProperties{
			KubernetesVersion: spec.KubernetesVersion,
		},
	}

	tags := make(map[string]*string)
	for key, val := range spec.Tags {
		if val != "" {
			tags[key] = to.StringPtr(val)
		}
	}
	managedCluster.Tags = tags

	networkProfile := &containerservice.NetworkProfile{
		NetworkPolicy: containerservice.NetworkPolicy(to.String(spec.NetworkPolicy)),
	}

	networkProfile.NetworkPlugin = containerservice.Kubenet
	if to.String(spec.NetworkPlugin) != "" {
		networkProfile.NetworkPlugin = containerservice.NetworkPlugin(to.String(spec.NetworkPlugin))
	}

	networkProfile.LoadBalancerSku = containerservice.Standard
	if to.String(spec.LoadBalancerSKU) != "" {
		networkProfile.LoadBalancerSku = containerservice.LoadBalancerSku(to.String(spec.LoadBalancerSKU))
	}

	virtualNetworkResourceGroup := spec.ResourceGroup
	if containerservice.NetworkPlugin(to.String(spec.NetworkPlugin)) == containerservice.Azure {
		// If a virtual network resource group is set, use it, otherwise assume it is the same as the cluster
		if to.String(spec.VirtualNetworkResourceGroup) != "" {
			virtualNetworkResourceGroup = to.String(spec.VirtualNetworkResourceGroup)
		}

		networkProfile.DNSServiceIP = spec.NetworkDNSServiceIP
		networkProfile.DockerBridgeCidr = spec.NetworkDockerBridgeCIDR
		networkProfile.ServiceCidr = spec.NetworkServiceCIDR
		networkProfile.PodCidr = spec.NetworkPodCIDR
	}
	managedCluster.ManagedClusterProperties.NetworkProfile = networkProfile

	agentPoolProfiles := []containerservice.ManagedClusterAgentPoolProfile{}
	for _, np := range spec.NodePools {
		agentProfile := containerservice.ManagedClusterAgentPoolProfile{
			Name:         np.Name,
			Count:        np.Count,
			MaxPods:      np.MaxPods,
			OsDiskSizeGB: np.OsDiskSizeGB,
			OsDiskType:   containerservice.OSDiskType(np.OsDiskType),
			OsType:       containerservice.OSType(np.OsType),
			VMSize:       containerservice.VMSizeTypes(np.VMSize),
			Mode:         containerservice.AgentPoolMode(np.Mode),
		}

		agentProfile.OrchestratorVersion = spec.KubernetesVersion
		if to.String(np.OrchestratorVersion) != "" {
			agentProfile.OrchestratorVersion = np.OrchestratorVersion
		}

		if np.AvailabilityZones != nil && len(*np.AvailabilityZones) > 0 {
			agentProfile.AvailabilityZones = np.AvailabilityZones
		}

		if to.StringSlice(np.AvailabilityZones) != nil {
			agentProfile.AvailabilityZones = np.AvailabilityZones
		}

		if to.Bool(np.EnableAutoScaling) {
			agentProfile.EnableAutoScaling = np.EnableAutoScaling
			agentProfile.MaxCount = np.MaxCount
			agentProfile.MinCount = np.MinCount
		}

		if hasCustomVirtualNetwork(spec) {
			agentProfile.VnetSubnetID = to.StringPtr(fmt.Sprintf(
				"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s/subnets/%s",
				cred.SubscriptionID,
				virtualNetworkResourceGroup,
				to.String(spec.VirtualNetwork),
				to.String(spec.Subnet),
			))
		}

		agentPoolProfiles = append(agentPoolProfiles, agentProfile)
	}
	managedCluster.ManagedClusterProperties.AgentPoolProfiles = &agentPoolProfiles

	if hasLinuxProfile(spec) {
		managedCluster.ManagedClusterProperties.LinuxProfile = &containerservice.LinuxProfile{
			AdminUsername: spec.LinuxAdminUsername,
			SSH: &containerservice.SSHConfiguration{
				PublicKeys: &[]containerservice.SSHPublicKey{
					{
						KeyData: spec.LinuxSSHPublicKey,
					},
				},
			},
		}
	}

	// Get addon profile from config spec
	managedCluster.ManagedClusterProperties.AddonProfiles = map[string]*containerservice.ManagedClusterAddonProfile{}

	if hasHTTPApplicationRoutingSupport(spec) {
		managedCluster.ManagedClusterProperties.AddonProfiles["httpApplicationRouting"] = &containerservice.ManagedClusterAddonProfile{
			Enabled: spec.HTTPApplicationRouting,
		}
	}

	// Get monitoring from config spec
	if to.Bool(spec.Monitoring) {
		managedCluster.ManagedClusterProperties.AddonProfiles["omsAgent"] = &containerservice.ManagedClusterAddonProfile{
			Enabled: spec.Monitoring,
		}

		logAnalyticsWorkspaceResourceID, err := checkLogAnalyticsWorkspaceForMonitoring(ctx, workplacesClient,
			spec.ResourceLocation, spec.ResourceGroup, to.String(spec.LogAnalyticsWorkspaceGroup), to.String(spec.LogAnalyticsWorkspaceName))
		if err != nil {
			return nil, err
		}

		if !strings.HasPrefix(logAnalyticsWorkspaceResourceID, "/") {
			logAnalyticsWorkspaceResourceID = "/" + logAnalyticsWorkspaceResourceID
		}
		logAnalyticsWorkspaceResourceID = strings.TrimSuffix(logAnalyticsWorkspaceResourceID, "/")

		managedCluster.ManagedClusterProperties.AddonProfiles["omsAgent"].Config = map[string]*string{
			"logAnalyticsWorkspaceResourceID": to.StringPtr(logAnalyticsWorkspaceResourceID),
		}
	}

	if phase != "updating" && phase != "active" {
		managedCluster.ServicePrincipalProfile = &containerservice.ManagedClusterServicePrincipalProfile{
			ClientID: to.StringPtr(cred.ClientID),
			Secret:   to.StringPtr(cred.ClientSecret),
		}
	}

	if to.String(spec.DNSPrefix) != "" {
		managedCluster.DNSPrefix = spec.DNSPrefix
	}

	if spec.AuthorizedIPRanges != nil {
		if managedCluster.APIServerAccessProfile == nil {
			managedCluster.APIServerAccessProfile = &containerservice.ManagedClusterAPIServerAccessProfile{}
		}
		managedCluster.APIServerAccessProfile.AuthorizedIPRanges = spec.AuthorizedIPRanges
	}

	if to.Bool(spec.PrivateCluster) {
		if managedCluster.APIServerAccessProfile == nil {
			managedCluster.APIServerAccessProfile = &containerservice.ManagedClusterAPIServerAccessProfile{}
		}
		managedCluster.APIServerAccessProfile.EnablePrivateCluster = spec.PrivateCluster
	}

	return managedCluster, nil
}

// CreateOrUpdateAgentPool creates a new pool(s) in AKS. If one already exists it updates the upstream node pool with
// any provided updates.
func CreateOrUpdateAgentPool(ctx context.Context, agentPoolClient services.AgentPoolsClientInterface, spec *aksv1.AKSClusterConfigSpec, np *aksv1.AKSNodePool) error {
	agentProfile := &containerservice.ManagedClusterAgentPoolProfileProperties{
		Count:               np.Count,
		MaxPods:             np.MaxPods,
		OsDiskSizeGB:        np.OsDiskSizeGB,
		OsDiskType:          containerservice.OSDiskType(np.OsDiskType),
		OsType:              containerservice.OSType(np.OsType),
		VMSize:              containerservice.VMSizeTypes(np.VMSize),
		Mode:                containerservice.AgentPoolMode(np.Mode),
		Type:                containerservice.VirtualMachineScaleSets,
		OrchestratorVersion: np.OrchestratorVersion,
		AvailabilityZones:   np.AvailabilityZones,
		EnableAutoScaling:   np.EnableAutoScaling,
		MinCount:            np.MinCount,
		MaxCount:            np.MaxCount,
	}

	_, err := agentPoolClient.CreateOrUpdate(ctx, spec.ResourceGroup, spec.ClusterName, to.String(np.Name), containerservice.AgentPool{
		ManagedClusterAgentPoolProfileProperties: agentProfile,
	})

	return err
}

func hasCustomVirtualNetwork(spec *aksv1.AKSClusterConfigSpec) bool {
	return spec.VirtualNetwork != nil && spec.Subnet != nil
}

func hasLinuxProfile(spec *aksv1.AKSClusterConfigSpec) bool {
	return spec.LinuxAdminUsername != nil && spec.LinuxSSHPublicKey != nil
}

func hasHTTPApplicationRoutingSupport(spec *aksv1.AKSClusterConfigSpec) bool {
	// HttpApplicationRouting is not supported in azure china cloud
	return !strings.HasPrefix(spec.ResourceLocation, "china")
}
