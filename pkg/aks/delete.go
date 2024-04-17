package aks

import (
	"context"

	"github.com/rancher/aks-operator/pkg/aks/services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	"github.com/sirupsen/logrus"
)

// RemoveCluster Delete AKS managed Kubernetes cluster
func RemoveCluster(ctx context.Context, clusterClient services.ManagedClustersClientInterface, spec *aksv1.AKSClusterConfigSpec) error {
	poller, err := clusterClient.BeginDelete(ctx, spec.ResourceGroup, spec.ClusterName, nil)
	if err != nil {
		return err
	}

	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		logrus.Errorf("can't get the AKS cluster deletion response: %v", err)
		return err
	}

	logrus.Infof("Cluster %v removed successfully", spec.ClusterName)
	logrus.Debugf("Cluster removal status %v", resp)

	return nil
}

// RemoveAgentPool Delete AKS Agent Pool
func RemoveAgentPool(ctx context.Context, agentPoolClient services.AgentPoolsClientInterface, spec *aksv1.AKSClusterConfigSpec, np *aksv1.AKSNodePool) error {
	_, err := agentPoolClient.BeginDelete(ctx, spec.ResourceGroup, spec.ClusterName, *np.Name)

	return err
}
