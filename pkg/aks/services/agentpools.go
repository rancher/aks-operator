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

type AgentPoolsClientInterface interface {
	BeginCreateOrUpdate(ctx context.Context, resourceGroupName string, clusterName string, agentPoolName string, parameters armcontainerservice.AgentPool) (*runtime.Poller[armcontainerservice.AgentPoolsClientCreateOrUpdateResponse], error)
	BeginDelete(ctx context.Context, resourceGroupName string, clusterName string, agentPoolName string) (*runtime.Poller[armcontainerservice.AgentPoolsClientDeleteResponse], error)
}

type agentPoolClient struct {
	agentPoolClient *armcontainerservice.AgentPoolsClient
}

func NewAgentPoolClient(subscriptionID string, credential *azidentity.ClientSecretCredential, cloud cloud.Configuration) (*agentPoolClient, error) {
	options := arm.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Cloud: cloud,
		},
	}
	clientFactory, err := armcontainerservice.NewClientFactory(subscriptionID, credential, &options)
	if err != nil {
		return nil, err
	}

	return &agentPoolClient{
		agentPoolClient: clientFactory.NewAgentPoolsClient(),
	}, nil
}

func (cl *agentPoolClient) BeginCreateOrUpdate(ctx context.Context, resourceGroupName string, clusterName string, agentPoolName string, parameters armcontainerservice.AgentPool) (*runtime.Poller[armcontainerservice.AgentPoolsClientCreateOrUpdateResponse], error) {
	return cl.agentPoolClient.BeginCreateOrUpdate(ctx, resourceGroupName, clusterName, agentPoolName, parameters, nil)
}

func (cl *agentPoolClient) BeginDelete(ctx context.Context, resourceGroupName string, clusterName string, agentPoolName string) (*runtime.Poller[armcontainerservice.AgentPoolsClientDeleteResponse], error) {
	return cl.agentPoolClient.BeginDelete(ctx, resourceGroupName, clusterName, agentPoolName, nil)
}
