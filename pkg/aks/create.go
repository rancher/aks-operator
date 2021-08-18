package aks

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/Azure/azure-sdk-for-go/services/resources/mgmt/2019-10-01/resources"
	"github.com/Azure/go-autorest/autorest/to"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
)

func CreateResourceGroup(ctx context.Context, groupsClient *resources.GroupsClient, spec *aksv1.AKSClusterConfigSpec) error {
	_, err := groupsClient.CreateOrUpdate(
		ctx,
		spec.ResourceGroup,
		resources.Group{
			Name:     to.StringPtr(spec.ResourceGroup),
			Location: to.StringPtr(spec.ResourceLocation),
		})

	return err
}

// CreateOrUpdateCluster creates a new managed Kubernetes cluster
func CreateOrUpdateCluster(ctx context.Context, cred *Credentials, clusterClient *containerservice.ManagedClustersClient,
	spec *aksv1.AKSClusterConfigSpec, phase string) error {

	tags := make(map[string]*string)
	for key, val := range spec.Tags {
		if val != "" {
			tags[key] = to.StringPtr(val)
		}
	}

	var vmNetSubnetID *string
	networkProfile := &containerservice.NetworkProfile{
		NetworkPlugin:   containerservice.Kubenet,
		NetworkPolicy:   containerservice.NetworkPolicy(to.String(spec.NetworkPolicy)),
		LoadBalancerSku: containerservice.Standard,
	}

	if spec.LoadBalancerSKU != nil {
		networkProfile.LoadBalancerSku = containerservice.LoadBalancerSku(to.String(spec.LoadBalancerSKU))
	}

	if containerservice.NetworkPlugin(to.String(spec.NetworkPlugin)) == containerservice.Azure {
		networkProfile.NetworkPlugin = containerservice.NetworkPlugin(to.String(spec.NetworkPlugin))
		virtualNetworkResourceGroup := spec.ResourceGroup

		//if virtual network resource group is set, use it, otherwise assume it is the same as the cluster
		if spec.VirtualNetworkResourceGroup != nil {
			virtualNetworkResourceGroup = to.String(spec.VirtualNetworkResourceGroup)
		}

		vmNetSubnetID = to.StringPtr(fmt.Sprintf(
			"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s/subnets/%s",
			cred.SubscriptionID,
			virtualNetworkResourceGroup,
			to.String(spec.VirtualNetwork),
			to.String(spec.Subnet),
		))

		networkProfile.DNSServiceIP = spec.NetworkDNSServiceIP
		networkProfile.DockerBridgeCidr = spec.NetworkDockerBridgeCIDR
		networkProfile.ServiceCidr = spec.NetworkServiceCIDR
		networkProfile.PodCidr = spec.NetworkPodCIDR
	}

	agentPoolProfiles := make([]containerservice.ManagedClusterAgentPoolProfile, 0, len(spec.NodePools))
	for _, np := range spec.NodePools {
		if np.OrchestratorVersion == nil {
			np.OrchestratorVersion = spec.KubernetesVersion
		}
		agentProfile := containerservice.ManagedClusterAgentPoolProfile{
			Name:                np.Name,
			Count:               np.Count,
			MaxPods:             np.MaxPods,
			OsDiskSizeGB:        np.OsDiskSizeGB,
			OsDiskType:          containerservice.OSDiskType(np.OsDiskType),
			OsType:              containerservice.OSType(np.OsType),
			VMSize:              containerservice.VMSizeTypes(np.VMSize),
			Mode:                containerservice.AgentPoolMode(np.Mode),
			OrchestratorVersion: np.OrchestratorVersion,
			AvailabilityZones:   np.AvailabilityZones,
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

	var linuxProfile *containerservice.LinuxProfile
	if hasLinuxProfile(spec) {
		linuxProfile = &containerservice.LinuxProfile{
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

	addonProfiles := make(map[string]*containerservice.ManagedClusterAddonProfile, 0)

	if hasHTTPApplicationRoutingSupport(spec) {
		addonProfiles["httpApplicationRouting"] = &containerservice.ManagedClusterAddonProfile{
			Enabled: spec.HTTPApplicationRouting,
		}
	}

	if to.Bool(spec.Monitoring) {
		addonProfiles["omsAgent"] = &containerservice.ManagedClusterAddonProfile{
			Enabled: spec.Monitoring,
		}

		operationInsightsWorkspaceClient, err := NewOperationInsightsWorkspaceClient(cred)
		if err != nil {
			return err
		}

		logAnalyticsWorkspaceResourceID, err := CheckLogAnalyticsWorkspaceForMonitoring(ctx, operationInsightsWorkspaceClient,
			spec.ResourceLocation, spec.ResourceGroup, to.String(spec.LogAnalyticsWorkspaceGroup), to.String(spec.LogAnalyticsWorkspaceName))
		if err != nil {
			return err
		}

		if !strings.HasPrefix(logAnalyticsWorkspaceResourceID, "/") {
			logAnalyticsWorkspaceResourceID = "/" + logAnalyticsWorkspaceResourceID
		}
		logAnalyticsWorkspaceResourceID = strings.TrimSuffix(logAnalyticsWorkspaceResourceID, "/")

		addonProfiles["omsAgent"].Config = map[string]*string{
			"logAnalyticsWorkspaceResourceID": to.StringPtr(logAnalyticsWorkspaceResourceID),
		}
	}

	managedCluster := containerservice.ManagedCluster{
		Name:     to.StringPtr(spec.ClusterName),
		Location: to.StringPtr(spec.ResourceLocation),
		Tags:     tags,
		ManagedClusterProperties: &containerservice.ManagedClusterProperties{
			KubernetesVersion: spec.KubernetesVersion,
			AgentPoolProfiles: &agentPoolProfiles,
			LinuxProfile:      linuxProfile,
			NetworkProfile:    networkProfile,
			AddonProfiles:     addonProfiles,
		},
	}

	if phase != "updating" && phase != "active" {
		managedCluster.ServicePrincipalProfile = &containerservice.ManagedClusterServicePrincipalProfile{
			ClientID: to.StringPtr(cred.ClientID),
			Secret:   to.StringPtr(cred.ClientSecret),
		}
	}

	if spec.DNSPrefix != nil {
		managedCluster.DNSPrefix = spec.DNSPrefix
	}

	if spec.AuthorizedIPRanges != nil {
		managedCluster.APIServerAccessProfile = &containerservice.ManagedClusterAPIServerAccessProfile{
			AuthorizedIPRanges: spec.AuthorizedIPRanges,
		}
	}
	if to.Bool(spec.PrivateCluster) {
		managedCluster.APIServerAccessProfile = &containerservice.ManagedClusterAPIServerAccessProfile{
			EnablePrivateCluster: spec.PrivateCluster,
		}
	}

	_, err := clusterClient.CreateOrUpdate(
		ctx,
		spec.ResourceGroup,
		spec.ClusterName,
		managedCluster,
	)

	return err
}

func CreateOrUpdateAgentPool(ctx context.Context, agentPoolClient *containerservice.AgentPoolsClient, spec *aksv1.AKSClusterConfigSpec, np *aksv1.AKSNodePool) error {
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
