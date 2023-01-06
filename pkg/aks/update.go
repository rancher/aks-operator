package aks

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/rancher/aks-operator/pkg/aks/services"
)

func UpdateClusterTags(ctx context.Context, clusterClient services.ManagedClustersClientInterface, resourceGroupName string, resourceName string, parameters containerservice.TagsObject) (containerservice.ManagedClustersUpdateTagsFuture, error) {
	return clusterClient.UpdateTags(ctx, resourceGroupName, resourceName, parameters)
}
