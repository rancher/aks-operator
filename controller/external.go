package controller

import (
	"context"

	"github.com/rancher/aks-operator/pkg/aks"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	wranglerv1 "github.com/rancher/wrangler/pkg/generated/controllers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// GetClusterKubeConfig returns a kubeconfig for a given cluster. This function is imported in rancher/rancher and has to stay for compatibility.
func GetClusterKubeConfig(ctx context.Context, secretsCache wranglerv1.SecretCache, secretClient wranglerv1.SecretClient, spec *aksv1.AKSClusterConfigSpec) (restConfig *rest.Config, err error) {
	credentials, err := aks.GetSecrets(secretsCache, secretClient, spec)
	if err != nil {
		return nil, err
	}
	resourceClusterClient, err := aks.NewClusterClient(credentials)
	if err != nil {
		return nil, err
	}
	accessProfile, err := resourceClusterClient.GetAccessProfile(ctx, spec.ResourceGroup, spec.ClusterName, "clusterAdmin")
	if err != nil {
		return nil, err
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(*accessProfile.KubeConfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}
