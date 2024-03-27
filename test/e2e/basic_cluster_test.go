package e2e

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	managementv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("BasicCluster", func() {
	var aksConfig *aksv1.AKSClusterConfig
	var cluster *managementv3.Cluster

	BeforeEach(func() {
		var ok bool
		aksConfig, ok = clusterTemplates[basicClusterTemplateName]
		Expect(ok).To(BeTrue())
		Expect(aksConfig).NotTo(BeNil())

		cluster = &managementv3.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: aksConfig.Name,
			},
			Spec: managementv3.ClusterSpec{
				AKSConfig: &aksConfig.Spec,
			},
		}

	})

	It("Succesfully creates a cluster", func() {
		By("Creating a cluster")
		Expect(cl.Create(ctx, cluster)).Should(Succeed())

		By("Waiting for cluster to be ready")
		Eventually(func() error {
			currentCluster := &aksv1.AKSClusterConfig{}

			if err := cl.Get(ctx, runtimeclient.ObjectKey{
				Name:      cluster.Name,
				Namespace: aksClusterConfigNamespace,
			}, currentCluster); err != nil {
				return err
			}

			if currentCluster.Status.Phase == "active" {
				return nil
			}

			return fmt.Errorf("cluster is not ready yet. Current phase: %s", currentCluster.Status.Phase)
		}, waitLong, pollInterval).ShouldNot(HaveOccurred())
	})

	It("Successfully adds and removes a node pool", func() {
		initialNodePools := aksConfig.DeepCopy().Spec.NodePools // save to restore later and test deletion

		Expect(cl.Get(ctx, runtimeclient.ObjectKey{Name: cluster.Name}, cluster)).Should(Succeed())
		patch := runtimeclient.MergeFrom(cluster.DeepCopy())

		nodePool := aksv1.AKSNodePool{
			Name:                to.Ptr("pool1"),
			AvailabilityZones:   to.Ptr([]string{"1", "2", "3"}),
			MaxPods:             to.Ptr(int32(110)),
			Count:               to.Ptr(int32(1)),
			Mode:                "User",
			OrchestratorVersion: cluster.Spec.AKSConfig.KubernetesVersion,
			OsDiskSizeGB:        to.Ptr(int32(128)),
			OsDiskType:          "Linux",
			VMSize:              "Standard_D2_v2",
		}
		cluster.Spec.AKSConfig.NodePools = append(cluster.Spec.AKSConfig.NodePools, nodePool)

		Expect(cl.Patch(ctx, cluster, patch)).Should(Succeed())

		By("Waiting for cluster to start adding node pool")
		Eventually(func() error {
			currentCluster := &aksv1.AKSClusterConfig{}

			if err := cl.Get(ctx, runtimeclient.ObjectKey{
				Name:      cluster.Name,
				Namespace: aksClusterConfigNamespace,
			}, currentCluster); err != nil {
				return err
			}

			if currentCluster.Status.Phase == "updating" && len(currentCluster.Spec.NodePools) == 2 {
				return nil
			}

			return fmt.Errorf("cluster didn't get new node pool. Current phase: %s, node pool count %d", currentCluster.Status.Phase, len(currentCluster.Spec.NodePools))
		}, waitLong, pollInterval).ShouldNot(HaveOccurred())

		By("Waiting for cluster to finish adding node pool")
		Eventually(func() error {
			currentCluster := &aksv1.AKSClusterConfig{}

			if err := cl.Get(ctx, runtimeclient.ObjectKey{
				Name:      cluster.Name,
				Namespace: aksClusterConfigNamespace,
			}, currentCluster); err != nil {
				return err
			}

			if currentCluster.Status.Phase == "active" && len(currentCluster.Spec.NodePools) == 2 {
				return nil
			}

			return fmt.Errorf("cluster didn't finish adding node pool. Current phase: %s, node pool count %d", currentCluster.Status.Phase, len(currentCluster.Spec.NodePools))
		}, waitLong, pollInterval).ShouldNot(HaveOccurred())

		By("Restoring initial node pools")

		Expect(cl.Get(ctx, runtimeclient.ObjectKey{Name: cluster.Name}, cluster)).Should(Succeed())
		patch = runtimeclient.MergeFrom(cluster.DeepCopy())

		cluster.Spec.AKSConfig.NodePools = initialNodePools

		Expect(cl.Patch(ctx, cluster, patch)).Should(Succeed())

		By("Waiting for cluster to start removing node pool")
		Eventually(func() error {
			currentCluster := &aksv1.AKSClusterConfig{}

			if err := cl.Get(ctx, runtimeclient.ObjectKey{
				Name:      cluster.Name,
				Namespace: aksClusterConfigNamespace,
			}, currentCluster); err != nil {
				return err
			}

			if currentCluster.Status.Phase == "updating" && len(currentCluster.Spec.NodePools) == 1 {
				return nil
			}

			return fmt.Errorf("cluster didn't start removing node pool. Current phase: %s, node pool count %d", currentCluster.Status.Phase, len(currentCluster.Spec.NodePools))
		}, waitLong, pollInterval).ShouldNot(HaveOccurred())

		By("Waiting for cluster to finish removing node pool")
		Eventually(func() error {
			currentCluster := &aksv1.AKSClusterConfig{}

			if err := cl.Get(ctx, runtimeclient.ObjectKey{
				Name:      cluster.Name,
				Namespace: aksClusterConfigNamespace,
			}, currentCluster); err != nil {
				return err
			}

			if currentCluster.Status.Phase == "active" && len(currentCluster.Spec.NodePools) == 1 {
				return nil
			}

			return fmt.Errorf("cluster didn't finish removing node pool. Current phase: %s, node pool count %d", currentCluster.Status.Phase, len(currentCluster.Spec.NodePools))
		}, waitLong, pollInterval).ShouldNot(HaveOccurred())

		By("Done waiting for cluster to finish removing node pool")
	})
})
