package aks

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	"github.com/rancher/aks-operator/pkg/aks/services"
)

func GetClusterAccessProfile(ctx context.Context, clusterClient services.ManagedClustersClientInterface, resourceGroupName string, resourceName string, roleName string) (armcontainerservice.ManagedClustersClientGetAccessProfileResponse, error) {
	return clusterClient.GetAccessProfile(ctx, resourceGroupName, resourceName, roleName, nil)
}
