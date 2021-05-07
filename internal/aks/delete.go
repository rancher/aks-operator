package aks

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/Azure/go-autorest/autorest/to"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	"github.com/sirupsen/logrus"
)

// RemoveCluster Delete AKS managed Kubernetes cluster
func RemoveCluster(ctx context.Context, clusterClient *containerservice.ManagedClustersClient, spec *aksv1.AKSClusterConfigSpec) error {
	future, err := clusterClient.Delete(ctx, spec.ResourceGroup, spec.ClusterName)
	if err != nil {
		return err
	}

	err = future.WaitForCompletionRef(ctx, clusterClient.Client)
	if err != nil {
		logrus.Errorf("can't get the AKS cluster create or update future response: %v", err)
		return err
	}

	logrus.Infof("Cluster %v removed successfully", spec.ClusterName)
	logrus.Debugf("Cluster removal status %v", future.Status())

	return nil
}

// RemoveAgentPool Delete AKS Agent Pool
func RemoveAgentPool(ctx context.Context, agentPoolClient *containerservice.AgentPoolsClient, spec *aksv1.AKSClusterConfigSpec, np *aksv1.AKSNodePool) error {
	_, err := agentPoolClient.Delete(ctx, spec.ResourceGroup, spec.ClusterName, to.String(np.Name))

	return err
}
