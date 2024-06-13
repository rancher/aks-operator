package controller

import (
	"context"
	"fmt"

	"github.com/rancher/aks-operator/pkg/aks"
	"github.com/rancher/aks-operator/pkg/aks/services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	wranglerv1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"k8s.io/client-go/rest"
)

// GetClusterKubeConfig returns a kubeconfig for a given cluster. This function is imported in rancher/rancher and has to stay for compatibility.
func GetClusterKubeConfig(ctx context.Context, secretsCache wranglerv1.SecretCache, secretClient wranglerv1.SecretClient, spec *aksv1.AKSClusterConfigSpec) (restConfig *rest.Config, err error) {
	credentials, err := aks.GetSecrets(secretsCache, secretClient, spec)
	if err != nil {
		return nil, fmt.Errorf("error getting credentials secret: %w", err)
	}

	clientSecretCredential, err := aks.NewClientSecretCredential(credentials)
	if err != nil {
		return nil, fmt.Errorf("error creating client secret credential: %w", err)
	}

	clustersClient, err := services.NewManagedClustersClient(credentials.SubscriptionID, clientSecretCredential, credentials.Cloud)
	if err != nil {
		return nil, fmt.Errorf("error creating managed cluster client: %w", err)
	}

	h := Handler{
		azureClients: azureClients{
			clustersClient: clustersClient,
		},
	}
	return h.getClusterKubeConfig(ctx, spec)
}

// BuildUpstreamClusterState creates an AKSClusterConfigSpec (spec for the AKS cluster state) from the existing
// cluster configuration.
func BuildUpstreamClusterState(ctx context.Context, secretsCache wranglerv1.SecretCache, secretClient wranglerv1.SecretClient, spec *aksv1.AKSClusterConfigSpec) (*aksv1.AKSClusterConfigSpec, error) {
	credentials, err := aks.GetSecrets(secretsCache, secretClient, spec)
	if err != nil {
		return nil, fmt.Errorf("error getting credentials secret: %w", err)
	}

	clientSecretCredential, err := aks.NewClientSecretCredential(credentials)
	if err != nil {
		return nil, fmt.Errorf("error creating client secret credential: %w", err)
	}

	clustersClient, err := services.NewManagedClustersClient(credentials.SubscriptionID, clientSecretCredential, credentials.Cloud)
	if err != nil {
		return nil, fmt.Errorf("error creating managed cluster client: %w", err)
	}

	h := Handler{
		secretsCache: secretsCache,
		secrets:      secretClient,
		azureClients: azureClients{
			clustersClient: clustersClient,
		},
	}
	return h.buildUpstreamClusterState(ctx, credentials, spec)
}
