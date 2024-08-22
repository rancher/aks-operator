package aks

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	"github.com/rancher/aks-operator/pkg/aks/services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	"github.com/sirupsen/logrus"
)

func UpdateClusterTags(ctx context.Context, clusterClient services.ManagedClustersClientInterface, resourceGroupName string, resourceName string, parameters armcontainerservice.TagsObject) (armcontainerservice.ManagedClustersClientUpdateTagsResponse, error) {
	poller, err := clusterClient.BeginUpdateTags(ctx, resourceGroupName, resourceName, parameters, nil)
	if err != nil {
		return armcontainerservice.ManagedClustersClientUpdateTagsResponse{}, err
	}

	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		logrus.Errorf("can't update the AKS cluster tags with error: %v", err)
		return armcontainerservice.ManagedClustersClientUpdateTagsResponse{}, err
	}
	return resp, nil
}

// UpdateCluster updates an existing managed Kubernetes cluster. Before updating, it pulls any existing configuration
// and then only updates managed fields.
func UpdateCluster(ctx context.Context, cred *Credentials, clusterClient services.ManagedClustersClientInterface, workplaceClient services.WorkplacesClientInterface,
	spec *aksv1.AKSClusterConfigSpec, phase string) error {
	// Create a new managed cluster from the AKS cluster config
	desiredCluster, err := createManagedCluster(ctx, cred, workplaceClient, spec, phase)
	if err != nil {
		return err
	}

	// Pull the upstream cluster state
	actualCluster, err := clusterClient.Get(ctx, spec.ResourceGroup, spec.ClusterName, nil)
	if err != nil {
		logrus.Errorf("error getting upstream AKS cluster by name [%s]: %s", spec.ClusterName, err.Error())
		return err
	}

	// Upstream cluster state was successfully pulled. Merge in updates without overwriting upstream fields: we never
	// want to overwrite preconfigured values in Azure with nil values. So only update fields pulled from AKS with
	// values from the managed cluster if they are non nil.

	poller, err := clusterClient.BeginCreateOrUpdate(
		ctx,
		spec.ResourceGroup,
		spec.ClusterName,
		updateCluster(*desiredCluster, actualCluster.ManagedCluster, spec.Imported),
		nil,
	)
	if err != nil {
		logrus.Errorf("can't update the AKS cluster with error: %v", err)
		return err
	}

	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		logrus.Errorf("can't update the AKS cluster with error: %v", err)
	}

	return err
}

func updateCluster(desiredCluster armcontainerservice.ManagedCluster, actualCluster armcontainerservice.ManagedCluster, imported bool) armcontainerservice.ManagedCluster {
	if !validateUpdate(desiredCluster, actualCluster) {
		logrus.Warn("Not all cluster properties can be updated.")
	}

	if actualCluster.Properties == nil {
		actualCluster.Properties = &armcontainerservice.ManagedClusterProperties{}
	}

	// Update kubernetes version
	// If a cluster is imported, we may not have the kubernetes version set on the spec.
	if desiredCluster.Properties.KubernetesVersion != nil {
		actualCluster.Properties.KubernetesVersion = desiredCluster.Properties.KubernetesVersion
	}

	// Add/update agent pool profiles
	if actualCluster.Properties.AgentPoolProfiles == nil {
		actualCluster.Properties.AgentPoolProfiles = []*armcontainerservice.ManagedClusterAgentPoolProfile{}
	}

	for _, ap := range desiredCluster.Properties.AgentPoolProfiles {
		if !hasAgentPoolProfile(String(ap.Name), actualCluster.Properties.AgentPoolProfiles) {
			actualCluster.Properties.AgentPoolProfiles = append(actualCluster.Properties.AgentPoolProfiles, ap)
		}
	}

	// Add/update addon profiles (this will keep separate profiles added by AKS). This code will also add/update addon
	// profiles for http application routing and monitoring.
	if actualCluster.Properties.AddonProfiles == nil {
		actualCluster.Properties.AddonProfiles = map[string]*armcontainerservice.ManagedClusterAddonProfile{}
	}
	for profile := range desiredCluster.Properties.AddonProfiles {
		actualCluster.Properties.AddonProfiles[profile] = desiredCluster.Properties.AddonProfiles[profile]
	}

	// Auth IP ranges
	// note: there could be authorized IP ranges set in AKS that haven't propagated yet when this update is done. Add
	// ranges from Rancher to any ones already set in AKS.
	if actualCluster.Properties.APIServerAccessProfile == nil {
		actualCluster.Properties.APIServerAccessProfile = &armcontainerservice.ManagedClusterAPIServerAccessProfile{
			AuthorizedIPRanges: []*string{},
		}
	}

	// If the cluster is imported, we may not have the authorized IP ranges set on the spec.
	if desiredCluster.Properties.APIServerAccessProfile != nil && desiredCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges != nil {
		if !imported {
			actualCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges = desiredCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges
		} else {
			for i := range desiredCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges {
				ipRange := (desiredCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges)[i]

				if !hasAuthorizedIPRange(String(ipRange), actualCluster.Properties.APIServerAccessProfile) {
					actualCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges = append(actualCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges, ipRange)
				}
			}
		}
	} else {
		if !imported && actualCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges != nil {
			actualCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges = []*string{}
		}
	}

	// Linux profile
	if desiredCluster.Properties.LinuxProfile != nil {
		actualCluster.Properties.LinuxProfile = desiredCluster.Properties.LinuxProfile
	}

	// Network profile
	// Setting the DockerBridgeCidr field is no longer supported, see https://github.com/Azure/AKS/issues/3534
	if desiredCluster.Properties.NetworkProfile != nil {
		if actualCluster.Properties.NetworkProfile == nil {
			actualCluster.Properties.NetworkProfile = &armcontainerservice.NetworkProfile{}
		}

		if desiredCluster.Properties.NetworkProfile.NetworkPlugin != nil {
			actualCluster.Properties.NetworkProfile.NetworkPlugin = desiredCluster.Properties.NetworkProfile.NetworkPlugin
		}
		if desiredCluster.Properties.NetworkProfile.NetworkPolicy != nil {
			actualCluster.Properties.NetworkProfile.NetworkPolicy = desiredCluster.Properties.NetworkProfile.NetworkPolicy
		}
		if desiredCluster.Properties.NetworkProfile.NetworkMode != nil { // This is never set on the managed cluster so it will always be empty
			actualCluster.Properties.NetworkProfile.NetworkMode = desiredCluster.Properties.NetworkProfile.NetworkMode
		}
		if desiredCluster.Properties.NetworkProfile.DNSServiceIP != nil {
			actualCluster.Properties.NetworkProfile.DNSServiceIP = desiredCluster.Properties.NetworkProfile.DNSServiceIP
		}
		if desiredCluster.Properties.NetworkProfile.PodCidr != nil {
			actualCluster.Properties.NetworkProfile.PodCidr = desiredCluster.Properties.NetworkProfile.PodCidr
		}
		if desiredCluster.Properties.NetworkProfile.ServiceCidr != nil {
			actualCluster.Properties.NetworkProfile.ServiceCidr = desiredCluster.Properties.NetworkProfile.ServiceCidr
		}
		if desiredCluster.Properties.NetworkProfile.OutboundType != nil { // This is never set on the managed cluster so it will always be empty
			actualCluster.Properties.NetworkProfile.OutboundType = desiredCluster.Properties.NetworkProfile.OutboundType
		}
		if desiredCluster.Properties.NetworkProfile.LoadBalancerSKU != nil {
			actualCluster.Properties.NetworkProfile.LoadBalancerSKU = desiredCluster.Properties.NetworkProfile.LoadBalancerSKU
		}
		// LoadBalancerProfile is not configurable in Rancher so there won't be subfield conflicts. Just pull it from
		// the state in AKS.
	}

	// Service principal client id and secret
	if desiredCluster.Properties.ServicePrincipalProfile != nil {
		actualCluster.Properties.ServicePrincipalProfile = desiredCluster.Properties.ServicePrincipalProfile
	}

	// Tags
	if desiredCluster.Tags != nil {
		actualCluster.Tags = desiredCluster.Tags
	}

	return actualCluster
}

func validateUpdate(desiredCluster armcontainerservice.ManagedCluster, actualCluster armcontainerservice.ManagedCluster) bool {
	/*
		The following fields are managed in Rancher but are NOT configurable on update
		- Name
		- Location
		- DNSPrefix
		- EnablePrivateCluster
		- LoadBalancerProfile
	*/

	if desiredCluster.Name != nil && actualCluster.Name != nil && String(desiredCluster.Name) != String(actualCluster.Name) {
		logrus.Warnf("Cluster name update from [%s] to [%s] is not supported", *actualCluster.Name, *desiredCluster.Name)
		return false
	}

	if desiredCluster.Location != nil && actualCluster.Location != nil && String(desiredCluster.Location) != String(actualCluster.Location) {
		logrus.Warnf("Cluster location update from [%s] to [%s] is not supported", *actualCluster.Location, *desiredCluster.Location)
		return false
	}

	if desiredCluster.Properties != nil && actualCluster.Properties != nil &&
		desiredCluster.Properties.DNSPrefix != nil && actualCluster.Properties.DNSPrefix != nil &&
		String(desiredCluster.Properties.DNSPrefix) != String(actualCluster.Properties.DNSPrefix) {
		logrus.Warnf("Cluster DNS prefix update from [%s] to [%s] is not supported", *actualCluster.Properties.DNSPrefix, *desiredCluster.Properties.DNSPrefix)
		return false
	}

	if desiredCluster.Properties != nil && actualCluster.Properties != nil &&
		desiredCluster.Properties.APIServerAccessProfile != nil && actualCluster.Properties.APIServerAccessProfile != nil &&
		Bool(desiredCluster.Properties.APIServerAccessProfile.EnablePrivateCluster) != Bool(actualCluster.Properties.APIServerAccessProfile.EnablePrivateCluster) {
		logrus.Warn("Cluster can't be updated from private")
		return false
	}

	return true
}

func hasAgentPoolProfile(name string, agentPoolProfiles []*armcontainerservice.ManagedClusterAgentPoolProfile) bool {
	if agentPoolProfiles == nil {
		return false
	}

	for _, ap := range agentPoolProfiles {
		if String(ap.Name) == name {
			return true
		}
	}
	return false
}

func hasAuthorizedIPRange(name string, apiServerAccessProfile *armcontainerservice.ManagedClusterAPIServerAccessProfile) bool {
	if apiServerAccessProfile == nil || apiServerAccessProfile.AuthorizedIPRanges == nil {
		return false
	}

	for _, ipRange := range apiServerAccessProfile.AuthorizedIPRanges {
		if String(ipRange) == name {
			return true
		}
	}
	return false
}
