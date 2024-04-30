package services

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
)

type Poller[T any] interface {
	PollUntilDone(ctx context.Context, options *runtime.PollUntilDoneOptions) (T, error)
}

type ManagedClustersClientInterface interface {
	BeginCreateOrUpdate(ctx context.Context, resourceGroupName string, resourceName string, parameters armcontainerservice.ManagedCluster, options *armcontainerservice.ManagedClustersClientBeginCreateOrUpdateOptions) (Poller[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse], error)
	Get(ctx context.Context, resourceGroupName string, resourceName string, options *armcontainerservice.ManagedClustersClientGetOptions) (armcontainerservice.ManagedClustersClientGetResponse, error)
	BeginDelete(ctx context.Context, resourceGroupName string, resourceName string, options *armcontainerservice.ManagedClustersClientBeginDeleteOptions) (Poller[armcontainerservice.ManagedClustersClientDeleteResponse], error)
	GetAccessProfile(ctx context.Context, resourceGroupName string, resourceName string, roleName string, options *armcontainerservice.ManagedClustersClientGetAccessProfileOptions) (armcontainerservice.ManagedClustersClientGetAccessProfileResponse, error)
	BeginUpdateTags(ctx context.Context, resourceGroupName string, resourceName string, parameters armcontainerservice.TagsObject, options *armcontainerservice.ManagedClustersClientBeginUpdateTagsOptions) (Poller[armcontainerservice.ManagedClustersClientUpdateTagsResponse], error)
}

type managedClustersClient struct {
	armManagedClustersClient *armcontainerservice.ManagedClustersClient
}

func NewManagedClustersClient(subscriptionID string, credential *azidentity.ClientSecretCredential, cloud cloud.Configuration) (*managedClustersClient, error) {
	options := arm.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Cloud: cloud,
		},
	}
	clientFactory, err := armcontainerservice.NewClientFactory(subscriptionID, credential, &options)
	if err != nil {
		return nil, err
	}

	return &managedClustersClient{
		armManagedClustersClient: clientFactory.NewManagedClustersClient(),
	}, nil
}

func (cl *managedClustersClient) BeginCreateOrUpdate(ctx context.Context, resourceGroupName string, resourceName string, parameters armcontainerservice.ManagedCluster, options *armcontainerservice.ManagedClustersClientBeginCreateOrUpdateOptions) (Poller[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse], error) {
	return cl.armManagedClustersClient.BeginCreateOrUpdate(ctx, resourceGroupName, resourceName, parameters, options)
}

func (cl *managedClustersClient) Get(ctx context.Context, resourceGroupName string, resourceName string, options *armcontainerservice.ManagedClustersClientGetOptions) (armcontainerservice.ManagedClustersClientGetResponse, error) {
	return cl.armManagedClustersClient.Get(ctx, resourceGroupName, resourceName, options)
}

func (cl *managedClustersClient) BeginDelete(ctx context.Context, resourceGroupName string, resourceName string, options *armcontainerservice.ManagedClustersClientBeginDeleteOptions) (Poller[armcontainerservice.ManagedClustersClientDeleteResponse], error) {
	return cl.armManagedClustersClient.BeginDelete(ctx, resourceGroupName, resourceName, options)
}

func (cl *managedClustersClient) GetAccessProfile(ctx context.Context, resourceGroupName string, resourceName string, roleName string, options *armcontainerservice.ManagedClustersClientGetAccessProfileOptions) (armcontainerservice.ManagedClustersClientGetAccessProfileResponse, error) {
	return cl.armManagedClustersClient.GetAccessProfile(ctx, resourceGroupName, resourceName, roleName, options)
}

func (cl *managedClustersClient) BeginUpdateTags(ctx context.Context, resourceGroupName string, resourceName string, parameters armcontainerservice.TagsObject, options *armcontainerservice.ManagedClustersClientBeginUpdateTagsOptions) (Poller[armcontainerservice.ManagedClustersClientUpdateTagsResponse], error) {
	return cl.armManagedClustersClient.BeginUpdateTags(ctx, resourceGroupName, resourceName, parameters, options)
}
