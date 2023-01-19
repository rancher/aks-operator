package aks

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/rancher/aks-operator/pkg/aks/services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	"github.com/sirupsen/logrus"
)

func UpdateClusterTags(ctx context.Context, clusterClient services.ManagedClustersClientInterface, resourceGroupName string, resourceName string, parameters containerservice.TagsObject) (containerservice.ManagedClustersUpdateTagsFuture, error) {
	return clusterClient.UpdateTags(ctx, resourceGroupName, resourceName, parameters)
}

// UpdateCluster updates an existing managed Kubernetes cluster. Before updating, it pulls any existing configuration
// and then only updates managed fields.
func UpdateCluster(ctx context.Context, cred *Credentials, clusterClient services.ManagedClustersClientInterface, workplaceClient services.WorkplacesClientInterface,
	spec *aksv1.AKSClusterConfigSpec, phase string) error {

	// Create a new managed cluster from the AKS cluster config
	managedCluster, err := createManagedCluster(ctx, cred, workplaceClient, spec, phase)
	if err != nil {
		return err
	}

	// Pull the upstream cluster state
	aksCluster, err := clusterClient.Get(ctx, spec.ResourceGroup, spec.ClusterName)
	if err != nil {
		logrus.Errorf("Error getting upstream AKS cluster by name [%s]: %s", spec.ClusterName, err.Error())
		return err
	}

	// Upstream cluster state was successfully pulled. Merge in updates without overwriting upstream fields: we never
	// want to overwrite preconfigured values in Azure with nil values. So only update fields pulled from AKS with
	// values from the managed cluster if they are non nil.

	_, err = clusterClient.CreateOrUpdate(
		ctx,
		spec.ResourceGroup,
		spec.ClusterName,
		updateCluster(aksCluster, managedCluster),
	)

	return err
}

func updateCluster(aksCluster containerservice.ManagedCluster, managedCluster containerservice.ManagedCluster) containerservice.ManagedCluster {
	/*
		The following fields are managed in Rancher but are NOT configurable on update
		- Name
		- Location
		- DNSPrefix
		- EnablePrivateCluster
		- LoadBalancerProfile
	*/

	if aksCluster.ManagedClusterProperties == nil {
		aksCluster.ManagedClusterProperties = &containerservice.ManagedClusterProperties{}
	}

	// Update kubernetes version
	// If a cluster is imported, we may not have the kubernetes version set on the spec.
	if managedCluster.KubernetesVersion != nil {
		aksCluster.KubernetesVersion = managedCluster.KubernetesVersion
	}

	// Add/update agent pool profiles
	if aksCluster.AgentPoolProfiles == nil {
		aksCluster.AgentPoolProfiles = &[]containerservice.ManagedClusterAgentPoolProfile{}
	}

	if managedCluster.AgentPoolProfiles != nil {
		for _, ap := range *managedCluster.AgentPoolProfiles {
			if !hasAgentPoolProfile(ap.Name, aksCluster.AgentPoolProfiles) {
				*aksCluster.AgentPoolProfiles = append(*aksCluster.AgentPoolProfiles, ap)
			}
		}
	}

	// Add/update addon profiles (this will keep separate profiles added by AKS). This code will also add/update addon
	// profiles for http application routing and monitoring.
	if aksCluster.AddonProfiles == nil {
		aksCluster.AddonProfiles = map[string]*containerservice.ManagedClusterAddonProfile{}
	}
	for profile := range managedCluster.AddonProfiles {
		aksCluster.AddonProfiles[profile] = managedCluster.AddonProfiles[profile]
	}

	// Auth IP ranges
	// note: there could be authorized IP ranges set in AKS that haven't propagated yet when this update is done. Add
	// ranges from Rancher to any ones already set in AKS.
	if aksCluster.APIServerAccessProfile == nil {
		aksCluster.APIServerAccessProfile = &containerservice.ManagedClusterAPIServerAccessProfile{
			AuthorizedIPRanges: &[]string{},
		}
	}

	if managedCluster.APIServerAccessProfile != nil && managedCluster.APIServerAccessProfile.AuthorizedIPRanges != nil {

		for index := range *managedCluster.APIServerAccessProfile.AuthorizedIPRanges {
			ipRange := (*managedCluster.APIServerAccessProfile.AuthorizedIPRanges)[index]

			if !hasAuthorizedIPRange(ipRange, aksCluster.APIServerAccessProfile.AuthorizedIPRanges) {
				*aksCluster.APIServerAccessProfile.AuthorizedIPRanges = append(*aksCluster.APIServerAccessProfile.AuthorizedIPRanges, ipRange)
			}
		}
	}

	// Linux profile
	if managedCluster.LinuxProfile != nil {
		aksCluster.LinuxProfile = managedCluster.LinuxProfile
	}

	// Network profile
	if managedCluster.NetworkProfile != nil {
		np := managedCluster.NetworkProfile

		if aksCluster.NetworkProfile == nil {
			aksCluster.NetworkProfile = &containerservice.NetworkProfile{}
		}

		if np.NetworkPlugin != "" {
			aksCluster.NetworkProfile.NetworkPlugin = np.NetworkPlugin
		}
		if np.NetworkPolicy != "" {
			aksCluster.NetworkProfile.NetworkPolicy = np.NetworkPolicy
		}
		if np.NetworkMode != "" {
			aksCluster.NetworkProfile.NetworkMode = np.NetworkMode
		}
		if np.DNSServiceIP != nil {
			aksCluster.NetworkProfile.DNSServiceIP = np.DNSServiceIP
		}
		if np.DockerBridgeCidr != nil {
			aksCluster.NetworkProfile.DockerBridgeCidr = np.DockerBridgeCidr
		}
		if np.PodCidr != nil {
			aksCluster.NetworkProfile.PodCidr = np.PodCidr
		}
		if np.ServiceCidr != nil {
			aksCluster.NetworkProfile.ServiceCidr = np.ServiceCidr
		}
		if np.OutboundType != "" {
			aksCluster.NetworkProfile.OutboundType = np.OutboundType
		}
		if np.LoadBalancerSku != "" {
			aksCluster.NetworkProfile.LoadBalancerSku = np.LoadBalancerSku
		}
		// LoadBalancerProfile is not configurable in Rancher so there won't be subfield conflicts. Just pull it from
		// the state in AKS.
	}

	// Service principal client id and secret
	if managedCluster.ServicePrincipalProfile != nil { // operator phase is 'updating' or 'active'
		aksCluster.ServicePrincipalProfile = managedCluster.ServicePrincipalProfile
	}

	// Tags
	if managedCluster.Tags != nil {
		aksCluster.Tags = managedCluster.Tags
	}

	return aksCluster
}
