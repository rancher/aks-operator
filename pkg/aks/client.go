package aks

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/Azure/azure-sdk-for-go/services/operationalinsights/mgmt/2020-08-01/operationalinsights"
	"github.com/Azure/azure-sdk-for-go/services/resources/mgmt/2019-10-01/resources"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/to"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	"github.com/rancher/aks-operator/pkg/utils"
	"github.com/rancher/machine/drivers/azure/azureutil"
	wranglerv1 "github.com/rancher/wrangler/pkg/generated/controllers/core/v1"
)

type Credentials struct {
	AuthBaseURL    *string
	BaseURL        *string
	SubscriptionID string
	TenantID       string
	ClientID       string
	ClientSecret   string
}

func NewResourceGroupClient(cred *Credentials) (*resources.GroupsClient, error) {
	authorizer, err := NewClientAuthorizer(cred)
	if err != nil {
		return nil, err
	}

	client := resources.NewGroupsClientWithBaseURI(to.String(cred.BaseURL), cred.SubscriptionID)
	client.Authorizer = authorizer

	return &client, nil
}

func NewClusterClient(cred *Credentials) (*containerservice.ManagedClustersClient, error) {
	authorizer, err := NewClientAuthorizer(cred)
	if err != nil {
		return nil, err
	}

	client := containerservice.NewManagedClustersClientWithBaseURI(to.String(cred.BaseURL), cred.SubscriptionID)
	client.Authorizer = authorizer

	return &client, nil
}

func NewAgentPoolClient(cred *Credentials) (*containerservice.AgentPoolsClient, error) {
	authorizer, err := NewClientAuthorizer(cred)
	if err != nil {
		return nil, err
	}

	agentProfile := containerservice.NewAgentPoolsClientWithBaseURI(to.String(cred.BaseURL), cred.SubscriptionID)
	agentProfile.Authorizer = authorizer

	return &agentProfile, nil
}

func NewOperationInsightsWorkspaceClient(cred *Credentials) (*operationalinsights.WorkspacesClient, error) {
	authorizer, err := NewClientAuthorizer(cred)
	if err != nil {
		return nil, err
	}

	client := operationalinsights.NewWorkspacesClientWithBaseURI(to.String(cred.BaseURL), cred.SubscriptionID)
	client.Authorizer = authorizer

	return &client, nil
}

func NewClientAuthorizer(cred *Credentials) (autorest.Authorizer, error) {
	if cred.AuthBaseURL == nil {
		cred.AuthBaseURL = to.StringPtr(azure.PublicCloud.ActiveDirectoryEndpoint)
	}

	if cred.BaseURL == nil {
		cred.BaseURL = to.StringPtr(azure.PublicCloud.ResourceManagerEndpoint)
	}

	oauthConfig, err := adal.NewOAuthConfig(to.String(cred.AuthBaseURL), cred.TenantID)
	if err != nil {
		return nil, err
	}

	spToken, err := adal.NewServicePrincipalToken(*oauthConfig, cred.ClientID, cred.ClientSecret, to.String(cred.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("couldn't authenticate to Azure cloud with error: %v", err)
	}

	return autorest.NewBearerAuthorizer(spToken), nil
}

func GetSecrets(secretsCache wranglerv1.SecretCache, spec *aksv1.AKSClusterConfigSpec) (*Credentials, error) {
	var cred Credentials

	if spec.AzureCredentialSecret == "" {
		return nil, fmt.Errorf("secret name not provided")
	}

	ns, id := utils.ParseSecretName(spec.AzureCredentialSecret)
	secret, err := secretsCache.Get(ns, id)
	if err != nil {
		return nil, fmt.Errorf("couldn't find secret [%s] in namespace [%s]", id, ns)
	}

	tenantIDBytes := secret.Data["azurecredentialConfig-tenantId"]
	subscriptionIDBytes := secret.Data["azurecredentialConfig-subscriptionId"]
	clientIDBytes := secret.Data["azurecredentialConfig-clientId"]
	clientSecretBytes := secret.Data["azurecredentialConfig-clientSecret"]

	cannotBeNilError := "field [azurecredentialConfig-%s] must be provided in cloud credential"
	if subscriptionIDBytes == nil {
		return nil, fmt.Errorf(cannotBeNilError, "subscriptionId")
	}
	if clientIDBytes == nil {
		return nil, fmt.Errorf(cannotBeNilError, "clientId")
	}
	if clientSecretBytes == nil {
		return nil, fmt.Errorf(cannotBeNilError, "clientSecret")
	}

	cred.TenantID = string(tenantIDBytes)
	cred.SubscriptionID = string(subscriptionIDBytes)
	cred.ClientID = string(clientIDBytes)
	cred.ClientSecret = string(clientSecretBytes)
	cred.AuthBaseURL = spec.AuthBaseURL
	cred.BaseURL = spec.BaseURL

	if cred.TenantID == "" {
		cred.TenantID, err = azureutil.FindTenantID(context.Background(), azure.PublicCloud, string(cred.SubscriptionID))
		if err != nil {
			return nil, err
		}
	}

	return &cred, nil
}
