package services

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
)

type Pager[T any] interface {
	More() bool
	NextPage(ctx context.Context) (T, error)
}

type SubscriptionsClientInterface interface {
	NewListLocationsPager(subscriptionID string, options *armsubscriptions.ClientListLocationsOptions) Pager[armsubscriptions.ClientListLocationsResponse]
}

type subscriptionClient struct {
	subscriptionClient *armsubscriptions.Client
}

func NewSubscriptionClient(credential *azidentity.ClientSecretCredential, cloud cloud.Configuration) (*subscriptionClient, error) {
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

func (cl *subscriptionClient) NewListLocationsPager(subscriptionID string, options *armsubscriptions.ClientListLocationsOptions) Pager[armsubscriptions.ClientListLocationsResponse] {
	return cl.subscriptionClient.NewListLocationsPager(subscriptionID, options)
}
