package services

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
)

type SubscriptionsClientInterface interface {
	NewListLocationsPager(subscriptionID string, options *armsubscriptions.ClientListLocationsOptions) *runtime.Pager[armsubscriptions.ClientListLocationsResponse]
}

type subscriptionClient struct {
	subscriptionClient *armsubscriptions.Client
}

func NewSubscriptionClient(subscriptionID string, credential *azidentity.ClientSecretCredential, cloud cloud.Configuration) (*subscriptionClient, error) {
	options := arm.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Cloud: cloud,
		},
	}
	clientFactory, err := armsubscriptions.NewClientFactory(credential, &options)
	if err != nil {
		return nil, err
	}

	return &subscriptionClient{
		subscriptionClient: clientFactory.NewClient(),
	}, nil
}

func (cl *subscriptionClient) NewListLocationsPager(subscriptionID string, options *armsubscriptions.ClientListLocationsOptions) *runtime.Pager[armsubscriptions.ClientListLocationsResponse] {
	return cl.subscriptionClient.NewListLocationsPager(subscriptionID, options)
}
