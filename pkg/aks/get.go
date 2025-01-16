package aks

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	"github.com/rancher/aks-operator/pkg/aks/services"
)

func GetClusterAccessProfile(ctx context.Context, clusterClient services.ManagedClustersClientInterface, resourceGroupName string, resourceName string, roleName string) (armcontainerservice.ManagedClustersClientGetAccessProfileResponse, error) {
	return clusterClient.GetAccessProfile(ctx, resourceGroupName, resourceName, roleName, nil)
}

func GetRegionsWithAvailabilityZoneSupport(ctx context.Context, client services.SubscriptionsClientInterface, subscription string) (map[string]bool, error) {
	regionsWithAzSupport := make(map[string]bool)
	pager := client.NewListLocationsPager(subscription, &armsubscriptions.ClientListLocationsOptions{IncludeExtendedLocations: nil})
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return regionsWithAzSupport, fmt.Errorf("failed to advance page: %w", err)
		}
		for _, v := range page.Value {
			if v.Name != nil {
				regionsWithAzSupport[*v.Name] = len(v.AvailabilityZoneMappings) > 0
			}
		}
	}
	return regionsWithAzSupport, nil
}
