package services

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

type ResourceGroupsClientInterface interface {
	CreateOrUpdate(ctx context.Context, resourceGroupName string, resourceGroup armresources.ResourceGroup, options *armresources.ResourceGroupsClientCreateOrUpdateOptions) (armresources.ResourceGroupsClientCreateOrUpdateResponse, error)
	BeginDelete(ctx context.Context, resourceGroupName string, options *armresources.ResourceGroupsClientBeginDeleteOptions) (*runtime.Poller[armresources.ResourceGroupsClientDeleteResponse], error)
	CheckExistence(ctx context.Context, resourceGroupName string, options *armresources.ResourceGroupsClientCheckExistenceOptions) (armresources.ResourceGroupsClientCheckExistenceResponse, error)
}

type resourceGroupsClient struct {
	armresourcesGroupsClient *armresources.ResourceGroupsClient
}

func NewResourceGroupsClient(subscriptionID string, credential *azidentity.ClientSecretCredential, cloud cloud.Configuration) (*resourceGroupsClient, error) {
	options := arm.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Cloud: cloud,
		},
	}
	clientFactory, err := armresources.NewClientFactory(subscriptionID, credential, &options)
	if err != nil {
		return nil, err
	}

	return &resourceGroupsClient{
		armresourcesGroupsClient: clientFactory.NewResourceGroupsClient(),
	}, nil
}

func (cl *resourceGroupsClient) CreateOrUpdate(ctx context.Context, resourceGroupName string, resourceGroup armresources.ResourceGroup, options *armresources.ResourceGroupsClientCreateOrUpdateOptions) (armresources.ResourceGroupsClientCreateOrUpdateResponse, error) {
	return cl.armresourcesGroupsClient.CreateOrUpdate(ctx, resourceGroupName, resourceGroup, options)
}

func (cl *resourceGroupsClient) BeginDelete(ctx context.Context, resourceGroupName string, options *armresources.ResourceGroupsClientBeginDeleteOptions) (*runtime.Poller[armresources.ResourceGroupsClientDeleteResponse], error) {
	return cl.armresourcesGroupsClient.BeginDelete(ctx, resourceGroupName, options)
}

func (cl *resourceGroupsClient) CheckExistence(ctx context.Context, resourceGroupName string, options *armresources.ResourceGroupsClientCheckExistenceOptions) (armresources.ResourceGroupsClientCheckExistenceResponse, error) {
	return cl.armresourcesGroupsClient.CheckExistence(ctx, resourceGroupName, options)
}
