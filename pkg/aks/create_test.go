package aks

import (
	"errors"

	"github.com/Azure/azure-sdk-for-go/services/resources/mgmt/2019-10-01/resources"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher/aks-operator/pkg/aks/services/mock_services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
)

var _ = Describe("CreateResourceGroup", func() {
	var (
		mockCtrl          *gomock.Controller
		rgClientMock      *mock_services.MockResourceGroupsClientInterface
		resourceGroupName = "test-rg"
		location          = "eastus"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		rgClientMock = mock_services.NewMockResourceGroupsClientInterface(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("should succefully create a resource group", func() {

		rgClientMock.EXPECT().CreateOrUpdate(ctx, resourceGroupName, resources.Group{
			Name:     to.StringPtr(resourceGroupName),
			Location: to.StringPtr(location),
		}).Return(resources.Group{}, nil)

		Expect(CreateResourceGroup(ctx, rgClientMock, &aksv1.AKSClusterConfigSpec{
			ResourceGroup:    resourceGroupName,
			ResourceLocation: location,
		})).To(Succeed())
	})

	It("should catch error when resource group creation fails", func() {
		rgClientMock.EXPECT().CreateOrUpdate(ctx, resourceGroupName, resources.Group{
			Name:     to.StringPtr(resourceGroupName),
			Location: to.StringPtr(location),
		}).Return(resources.Group{}, errors.New("failed to create resource group"))

		err := CreateResourceGroup(ctx, rgClientMock, &aksv1.AKSClusterConfigSpec{
			ResourceGroup:    resourceGroupName,
			ResourceLocation: location,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create resource group"))
	})
})
