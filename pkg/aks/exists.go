package aks

import (
	"context"

	"github.com/rancher/aks-operator/pkg/aks/services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
)

func ExistsResourceGroup(ctx context.Context, groupsClient services.ResourceGroupsClientInterface, resourceGroup string) (bool, error) {
	resp, err := groupsClient.CheckExistence(ctx, resourceGroup, nil)
	if err != nil {
		return false, err
	}
	return resp.Success, err
}

// ExistsCluster Check if AKS managed Kubernetes cluster exist
func ExistsCluster(ctx context.Context, clusterClient services.ManagedClustersClientInterface, spec *aksv1.AKSClusterConfigSpec) (bool, error) {
	resp, err := clusterClient.Get(ctx, spec.ResourceGroup, spec.ClusterName, nil)
	if err != nil {
		return false, err
	}

	return String(resp.Name) == spec.ClusterName, err
}
