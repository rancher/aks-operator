package aks

import (
	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher/aks-operator/pkg/aks/services/mock_services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
)

var _ = Describe("RemoveCluster", func() {
	var (
		mockCtrl          *gomock.Controller
		clusterClientMock *mock_services.MockManagedClustersClientInterface
		clusterSpec       *aksv1.AKSClusterConfigSpec
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		clusterClientMock = mock_services.NewMockManagedClustersClientInterface(mockCtrl)
		clusterSpec = &aksv1.AKSClusterConfigSpec{
			ResourceGroup: "resourcegroup",
			ClusterName:   "clustername",
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("should successfully delete cluster", func() {
		clusterClientMock.EXPECT().Delete(ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName).Return(containerservice.ManagedClustersDeleteFuture{
			FutureAPI: &azure.Future{},
		}, nil)
		clusterClientMock.EXPECT().WaitForTaskCompletion(ctx, gomock.Any()).Return(nil)
		Expect(RemoveCluster(ctx, clusterClientMock, clusterSpec)).To(Succeed())
	})
})
