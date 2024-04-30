package aks

import (
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher/aks-operator/pkg/aks/services/mock_services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	"go.uber.org/mock/gomock"
)

var _ = Describe("RemoveCluster", func() {
	var (
		mockController    *gomock.Controller
		clusterClientMock *mock_services.MockManagedClustersClientInterface
		pollerMock        *mock_services.MockPoller[armcontainerservice.ManagedClustersClientDeleteResponse]
		clusterSpec       *aksv1.AKSClusterConfigSpec
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		clusterClientMock = mock_services.NewMockManagedClustersClientInterface(mockController)
		pollerMock = mock_services.NewMockPoller[armcontainerservice.ManagedClustersClientDeleteResponse](mockController)
		clusterSpec = &aksv1.AKSClusterConfigSpec{
			ResourceGroup: "resourcegroup",
			ClusterName:   "clustername",
		}
	})

	AfterEach(func() {
		mockController.Finish()
	})

	It("should successfully delete cluster", func() {
		clusterClientMock.EXPECT().BeginDelete(ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, nil).Return(pollerMock, nil)
		pollerMock.EXPECT().PollUntilDone(ctx, nil).Return(armcontainerservice.ManagedClustersClientDeleteResponse{}, nil)
		Expect(RemoveCluster(ctx, clusterClientMock, clusterSpec)).To(Succeed())
	})
})
