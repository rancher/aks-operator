package services

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/operationalinsights/armoperationalinsights"
)

type WorkplacesClientInterface interface {
	BeginCreateOrUpdate(ctx context.Context, resourceGroupName string, workspaceName string, parameters armoperationalinsights.Workspace, options *armoperationalinsights.WorkspacesClientBeginCreateOrUpdateOptions) (Poller[armoperationalinsights.WorkspacesClientCreateOrUpdateResponse], error)
	Get(ctx context.Context, resourceGroupName string, workspaceName string, options *armoperationalinsights.WorkspacesClientGetOptions) (armoperationalinsights.WorkspacesClientGetResponse, error)
}

type workplacesClient struct {
	armWorkspacesClient *armoperationalinsights.WorkspacesClient
}

func NewWorkplacesClient(subscriptionID string, credential *azidentity.ClientSecretCredential, cloud cloud.Configuration) (*workplacesClient, error) {
	options := arm.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Cloud: cloud,
		},
	}
	clientFactory, err := armoperationalinsights.NewClientFactory(subscriptionID, credential, &options)
	if err != nil {
		return nil, err
	}

	return &workplacesClient{
		armWorkspacesClient: clientFactory.NewWorkspacesClient(),
	}, nil
}

func (c *workplacesClient) BeginCreateOrUpdate(ctx context.Context, resourceGroupName string, workspaceName string, parameters armoperationalinsights.Workspace, options *armoperationalinsights.WorkspacesClientBeginCreateOrUpdateOptions) (Poller[armoperationalinsights.WorkspacesClientCreateOrUpdateResponse], error) {
	return c.armWorkspacesClient.BeginCreateOrUpdate(ctx, resourceGroupName, workspaceName, parameters, options)
}

func (c *workplacesClient) Get(ctx context.Context, resourceGroupName string, workspaceName string, options *armoperationalinsights.WorkspacesClientGetOptions) (armoperationalinsights.WorkspacesClientGetResponse, error) {
	return c.armWorkspacesClient.Get(ctx, resourceGroupName, workspaceName, options)
}
