package aks

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/rancher/aks-operator/pkg/aks/services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	"github.com/rancher/aks-operator/pkg/utils"
	"github.com/sirupsen/logrus"
)

const (
	maxNodeResourceGroupNameLength = 80
)

func CreateResourceGroup(ctx context.Context, groupsClient services.ResourceGroupsClientInterface, spec *aksv1.AKSClusterConfigSpec) error {
	_, err := groupsClient.CreateOrUpdate(
		ctx,
		spec.ResourceGroup,
		armresources.ResourceGroup{
			Name:     to.Ptr(spec.ResourceGroup),
			Location: to.Ptr(spec.ResourceLocation),
		},
		nil,
	)
	return err
}

// CreateCluster creates a new managed Kubernetes cluster. In this case, there will be no existing upstream cluster.
// We are provisioning a brand new one.
func CreateCluster(ctx context.Context, cred *Credentials, clusterClient services.ManagedClustersClientInterface, workplaceClient services.WorkplacesClientInterface,
	spec *aksv1.AKSClusterConfigSpec, phase string) error {
	managedCluster, err := createManagedCluster(ctx, cred, workplaceClient, spec, phase)
	if err != nil {
		return err
	}

	_, err = clusterClient.BeginCreateOrUpdate(
		ctx,
		spec.ResourceGroup,
		spec.ClusterName,
		*managedCluster,
		nil,
	)

	return err
}

// createManagedCluster creates a new managed Kubernetes cluster.
func createManagedCluster(ctx context.Context, cred *Credentials, workplacesClient services.WorkplacesClientInterface, spec *aksv1.AKSClusterConfigSpec, phase string) (*armcontainerservice.ManagedCluster, error) {
	managedCluster := &armcontainerservice.ManagedCluster{
		Name:     to.Ptr(spec.ClusterName),
		Location: to.Ptr(spec.ResourceLocation),
		Properties: &armcontainerservice.ManagedClusterProperties{
			KubernetesVersion: spec.KubernetesVersion,
		},
	}

	managedCluster.Tags = make(map[string]*string)
	for key, val := range spec.Tags {
		managedCluster.Tags[key] = to.Ptr(val)
	}

	nodeResourceGroupName := ""
	if String(spec.NodeResourceGroup) != "" {
		nodeResourceGroupName = String(spec.NodeResourceGroup)
	} else {
		nodeResourceGroupName = fmt.Sprintf("MC_%s_%s_%s", spec.ResourceGroup, spec.ClusterName, spec.ResourceLocation)
		if len(nodeResourceGroupName) > maxNodeResourceGroupNameLength {
			logrus.Infof("Default node resource group name [%s] is too long: truncating to %d characters: [%s]", nodeResourceGroupName, maxNodeResourceGroupNameLength, nodeResourceGroupName[:maxNodeResourceGroupNameLength])
			nodeResourceGroupName = nodeResourceGroupName[:maxNodeResourceGroupNameLength]
		}
	}
	managedCluster.Properties.NodeResourceGroup = to.Ptr(nodeResourceGroupName)

	networkProfile := &armcontainerservice.NetworkProfile{}

	switch strings.ToLower(String(spec.OutboundType)) {
	case strings.ToLower(string(armcontainerservice.OutboundTypeLoadBalancer)):
		networkProfile.OutboundType = to.Ptr(armcontainerservice.OutboundTypeLoadBalancer)
	case strings.ToLower(string(armcontainerservice.OutboundTypeUserDefinedRouting)):
		networkProfile.OutboundType = to.Ptr(armcontainerservice.OutboundTypeUserDefinedRouting)
	case strings.ToLower(string(armcontainerservice.OutboundTypeManagedNATGateway)):
		networkProfile.OutboundType = to.Ptr(armcontainerservice.OutboundTypeManagedNATGateway)
	case "":
		networkProfile.OutboundType = to.Ptr(armcontainerservice.OutboundTypeLoadBalancer)
	}

	switch strings.ToLower(String(spec.NetworkPolicy)) {
	case string(armcontainerservice.NetworkPolicyAzure):
		networkProfile.NetworkPolicy = to.Ptr(armcontainerservice.NetworkPolicyAzure)
	case string(armcontainerservice.NetworkPolicyCalico):
		networkProfile.NetworkPolicy = to.Ptr(armcontainerservice.NetworkPolicyCalico)
	case "":
		networkProfile.NetworkPolicy = nil
	default:
		return nil, fmt.Errorf("networkPolicy '%s' is not supported", String(spec.NetworkPolicy))
	}

	switch strings.ToLower(String(spec.NetworkPlugin)) {
	case string(armcontainerservice.NetworkPluginAzure):
		networkProfile.NetworkPlugin = to.Ptr(armcontainerservice.NetworkPluginAzure)
	case string(armcontainerservice.NetworkPluginKubenet):
		networkProfile.NetworkPlugin = to.Ptr(armcontainerservice.NetworkPluginKubenet)
	case "":
		networkProfile.NetworkPlugin = to.Ptr(armcontainerservice.NetworkPluginKubenet)
	default:
		return nil, fmt.Errorf("networkPlugin '%s' is not supported", String(spec.NetworkPlugin))
	}

	if *networkProfile.NetworkPlugin == armcontainerservice.NetworkPluginKubenet && String(spec.NetworkPolicy) == string(armcontainerservice.NetworkPolicyAzure) {
		return nil, fmt.Errorf("network plugin Kubenet is not compatible with network policy Azure")
	}

	networkProfile.LoadBalancerSKU = to.Ptr(armcontainerservice.LoadBalancerSKUStandard)

	if strings.EqualFold(String(spec.LoadBalancerSKU), string(armcontainerservice.LoadBalancerSKUBasic)) {
		logrus.Warnf("LoadBalancerSKU 'basic' is not supported")
		networkProfile.LoadBalancerSKU = to.Ptr(armcontainerservice.LoadBalancerSKUBasic)
	}

	// Disable standard loadbalancer for UserDefinedRouting and use routing created by user pre-defined table for egress
	if strings.EqualFold(String(spec.OutboundType), string(armcontainerservice.OutboundTypeUserDefinedRouting)) {
		networkProfile.LoadBalancerSKU = nil
	}

	virtualNetworkResourceGroup := spec.ResourceGroup
	if armcontainerservice.NetworkPlugin(String(spec.NetworkPlugin)) == armcontainerservice.NetworkPluginAzure || armcontainerservice.NetworkPlugin(String(spec.NetworkPlugin)) == armcontainerservice.NetworkPluginKubenet {
		// If a virtual network resource group is set, use it, otherwise assume it is the same as the cluster
		if String(spec.VirtualNetworkResourceGroup) != "" {
			virtualNetworkResourceGroup = String(spec.VirtualNetworkResourceGroup)
		}

		// Setting the DockerBridgeCidr field is no longer supported, see https://github.com/Azure/AKS/issues/3534
		networkProfile.DNSServiceIP = spec.NetworkDNSServiceIP
		networkProfile.ServiceCidr = spec.NetworkServiceCIDR
		networkProfile.PodCidr = spec.NetworkPodCIDR
	}
	managedCluster.Properties.NetworkProfile = networkProfile

	agentPoolProfiles := []*armcontainerservice.ManagedClusterAgentPoolProfile{}
	for _, np := range spec.NodePools {
		agentProfile := &armcontainerservice.ManagedClusterAgentPoolProfile{
			Name:         np.Name,
			Count:        np.Count,
			MaxPods:      np.MaxPods,
			OSDiskSizeGB: np.OsDiskSizeGB,
			OSDiskType:   to.Ptr(armcontainerservice.OSDiskType(np.OsDiskType)),
			OSType:       to.Ptr(armcontainerservice.OSType(np.OsType)),
			VMSize:       to.Ptr(np.VMSize),
			Mode:         to.Ptr(armcontainerservice.AgentPoolMode(np.Mode)),
			NodeLabels:   np.NodeLabels,
			NodeTaints:   utils.ConvertToSliceOfPointers(np.NodeTaints),
		}

		if np.MaxSurge != nil {
			agentProfile.UpgradeSettings = &armcontainerservice.AgentPoolUpgradeSettings{
				MaxSurge: np.MaxSurge,
			}
		}

		if np.NodeTaints != nil && len(*np.NodeTaints) > 0 {
			agentProfile.NodeTaints = utils.ConvertToSliceOfPointers(np.NodeTaints)
		}

		agentProfile.OrchestratorVersion = spec.KubernetesVersion
		if String(np.OrchestratorVersion) != "" {
			agentProfile.OrchestratorVersion = np.OrchestratorVersion
		}
		if np.AvailabilityZones != nil && len(*np.AvailabilityZones) > 0 {
			if !CheckAvailabilityZonesSupport(spec.ResourceLocation) {
				return nil, fmt.Errorf("availability zones are not supported in region %s", spec.ResourceLocation)
			}
			agentProfile.AvailabilityZones = utils.ConvertToSliceOfPointers(np.AvailabilityZones)
		}

		if StringSlice(np.AvailabilityZones) != nil {
			agentProfile.AvailabilityZones = utils.ConvertToSliceOfPointers(np.AvailabilityZones)
		}

		if Bool(np.EnableAutoScaling) {
			agentProfile.EnableAutoScaling = np.EnableAutoScaling
			agentProfile.MaxCount = np.MaxCount
			agentProfile.MinCount = np.MinCount
		}

		if hasCustomVirtualNetwork(spec) {
			agentProfile.VnetSubnetID = to.Ptr(fmt.Sprintf(
				"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s/subnets/%s",
				cred.SubscriptionID,
				virtualNetworkResourceGroup,
				String(spec.VirtualNetwork),
				String(spec.Subnet),
			))
		}

		agentPoolProfiles = append(agentPoolProfiles, agentProfile)
	}
	managedCluster.Properties.AgentPoolProfiles = agentPoolProfiles

	if hasLinuxProfile(spec) {
		managedCluster.Properties.LinuxProfile = &armcontainerservice.LinuxProfile{
			AdminUsername: spec.LinuxAdminUsername,
			SSH: &armcontainerservice.SSHConfiguration{
				PublicKeys: []*armcontainerservice.SSHPublicKey{
					{
						KeyData: spec.LinuxSSHPublicKey,
					},
				},
			},
		}
	}

	// Get addon profile from config spec
	managedCluster.Properties.AddonProfiles = map[string]*armcontainerservice.ManagedClusterAddonProfile{}

	if hasHTTPApplicationRoutingSupport(spec) {
		managedCluster.Properties.AddonProfiles["httpApplicationRouting"] = &armcontainerservice.ManagedClusterAddonProfile{
			Enabled: spec.HTTPApplicationRouting,
		}
	}

	// Get monitoring from config spec
	if spec.Monitoring != nil && Bool(spec.Monitoring) {
		logrus.Debug("Monitoring is enabled")
		managedCluster.Properties.AddonProfiles["omsAgent"] = &armcontainerservice.ManagedClusterAddonProfile{
			Enabled: spec.Monitoring,
		}

		logAnalyticsWorkspaceResourceID, err := CheckLogAnalyticsWorkspaceForMonitoring(ctx, workplacesClient,
			spec.ResourceLocation, spec.ResourceGroup, String(spec.LogAnalyticsWorkspaceGroup), String(spec.LogAnalyticsWorkspaceName))
		if err != nil {
			return nil, err
		}

		if !strings.HasPrefix(logAnalyticsWorkspaceResourceID, "/") {
			logAnalyticsWorkspaceResourceID = "/" + logAnalyticsWorkspaceResourceID
		}
		logAnalyticsWorkspaceResourceID = strings.TrimSuffix(logAnalyticsWorkspaceResourceID, "/")

		managedCluster.Properties.AddonProfiles["omsAgent"].Config = map[string]*string{
			"logAnalyticsWorkspaceResourceID": to.Ptr(logAnalyticsWorkspaceResourceID),
		}
	} else if spec.Monitoring != nil && !Bool(spec.Monitoring) {
		logrus.Debug("Monitoring is disabled")
		managedCluster.Properties.AddonProfiles["omsAgent"] = &armcontainerservice.ManagedClusterAddonProfile{
			Enabled: spec.Monitoring,
			Config:  nil,
		}
	}

	if phase != "updating" && phase != "active" {
		managedCluster.Properties.ServicePrincipalProfile = &armcontainerservice.ManagedClusterServicePrincipalProfile{
			ClientID: to.Ptr(cred.ClientID),
			Secret:   to.Ptr(cred.ClientSecret),
		}
	}

	if String(spec.DNSPrefix) != "" {
		managedCluster.Properties.DNSPrefix = spec.DNSPrefix
	}

	if spec.AuthorizedIPRanges != nil {
		if managedCluster.Properties.APIServerAccessProfile == nil {
			managedCluster.Properties.APIServerAccessProfile = &armcontainerservice.ManagedClusterAPIServerAccessProfile{}
		}
		managedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges = utils.ConvertToSliceOfPointers(spec.AuthorizedIPRanges)
	}

	if Bool(spec.PrivateCluster) {
		if managedCluster.Properties.APIServerAccessProfile == nil {
			managedCluster.Properties.APIServerAccessProfile = &armcontainerservice.ManagedClusterAPIServerAccessProfile{}
		}
		managedCluster.Properties.APIServerAccessProfile.EnablePrivateCluster = spec.PrivateCluster
		// Private DNS Zone ID can be set only for private cluster
		if spec.PrivateDNSZone != nil {
			managedCluster.Properties.APIServerAccessProfile.PrivateDNSZone = spec.PrivateDNSZone
		}
	}

	if cred.TenantID != "" {
		managedCluster.Identity = &armcontainerservice.ManagedClusterIdentity{
			Type: to.Ptr(armcontainerservice.ResourceIdentityTypeSystemAssigned),
		}
	}

	if Bool(spec.ManagedIdentity) {
		managedCluster.Identity = &armcontainerservice.ManagedClusterIdentity{
			Type: to.Ptr(armcontainerservice.ResourceIdentityTypeSystemAssigned),
		}
		if spec.UserAssignedIdentity != nil {
			managedCluster.Identity = &armcontainerservice.ManagedClusterIdentity{
				Type: to.Ptr(armcontainerservice.ResourceIdentityTypeUserAssigned),
				UserAssignedIdentities: map[string]*armcontainerservice.ManagedServiceIdentityUserAssignedIdentitiesValue{
					String(spec.UserAssignedIdentity): {},
				},
			}
		}
	}

	return managedCluster, nil
}

// CreateOrUpdateAgentPool creates a new pool(s) in AKS. If one already exists it updates the upstream node pool with
// any provided updates.
func CreateOrUpdateAgentPool(ctx context.Context, agentPoolClient services.AgentPoolsClientInterface, spec *aksv1.AKSClusterConfigSpec, np *aksv1.AKSNodePool) error {
	if np.AvailabilityZones != nil && len(*np.AvailabilityZones) > 0 && !CheckAvailabilityZonesSupport(spec.ResourceLocation) {
		return fmt.Errorf("availability zones are not supported in region %s", spec.ResourceLocation)
	}
	agentProfile := &armcontainerservice.ManagedClusterAgentPoolProfileProperties{
		Count:               np.Count,
		MaxPods:             np.MaxPods,
		OSDiskSizeGB:        np.OsDiskSizeGB,
		OSDiskType:          to.Ptr(armcontainerservice.OSDiskType(np.OsDiskType)),
		OSType:              to.Ptr(armcontainerservice.OSType(np.OsType)),
		VMSize:              to.Ptr(np.VMSize),
		Mode:                to.Ptr(armcontainerservice.AgentPoolMode(np.Mode)),
		Type:                to.Ptr(armcontainerservice.AgentPoolTypeVirtualMachineScaleSets),
		OrchestratorVersion: np.OrchestratorVersion,
		AvailabilityZones:   utils.ConvertToSliceOfPointers(np.AvailabilityZones),
		EnableAutoScaling:   np.EnableAutoScaling,
		MinCount:            np.MinCount,
		MaxCount:            np.MaxCount,
		VnetSubnetID:        np.VnetSubnetID,
		NodeLabels:          np.NodeLabels,
		NodeTaints:          utils.ConvertToSliceOfPointers(np.NodeTaints),
	}

	if np.MaxSurge != nil {
		agentProfile.UpgradeSettings = &armcontainerservice.AgentPoolUpgradeSettings{
			MaxSurge: np.MaxSurge,
		}
	}

	_, err := agentPoolClient.BeginCreateOrUpdate(ctx, spec.ResourceGroup, spec.ClusterName, String(np.Name), armcontainerservice.AgentPool{
		Properties: agentProfile,
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
