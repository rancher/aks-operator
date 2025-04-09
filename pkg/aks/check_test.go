package aks

import (
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/operationalinsights/armoperationalinsights"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher/aks-operator/pkg/aks/services/mock_services"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func Test_generateUniqueLogWorkspace(t *testing.T) {
	tests := []struct {
		name          string
		workspaceName string
		want          string
	}{
		{
			name:          "basic test",
			workspaceName: "ThisIsAValidInputklasjdfkljasjgireqahtawjsfklakjghrehtuirqhjfhwjkdfhjkawhfdjkhafvjkahg",
			want:          "ThisIsAValidInputklasjdfkljasjgireqahtawjsfkla-fb8fb22278d8eb98",
		},
	}
	for _, tt := range tests {
		got := generateUniqueLogWorkspace(tt.workspaceName)
		assert.Equal(t, tt.want, got)
		assert.Len(t, got, 63)
	}
}

var _ = Describe("CheckLogAnalyticsWorkspaceForMonitoring", func() {
	var (
		workplacesClientMock *mock_services.MockWorkplacesClientInterface
		pollerMock           *mock_services.MockPoller[armoperationalinsights.WorkspacesClientCreateOrUpdateResponse]
		mockController       *gomock.Controller
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		workplacesClientMock = mock_services.NewMockWorkplacesClientInterface(mockController)
		pollerMock = mock_services.NewMockPoller[armoperationalinsights.WorkspacesClientCreateOrUpdateResponse](mockController)
	})

	AfterEach(func() {
		mockController.Finish()
	})

	It("should return workspaceID if workspace already exists", func() {
		workspaceName := "workspaceName"
		workspaceResourceGroup := "resourcegroup"
		id := "workspaceID"
		workplacesClientMock.EXPECT().Get(ctx, workspaceResourceGroup, workspaceName, nil).Return(
			armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: &id,
				},
			}, nil)
		workspaceID, err := CheckLogAnalyticsWorkspaceForMonitoring(ctx,
			workplacesClientMock,
			"eastus", workspaceResourceGroup, "", workspaceName)
		Expect(err).NotTo(HaveOccurred())
		Expect(workspaceID).To(Equal(id))
	})

	It("should create workspace if it doesn't exist", func() {
		workspaceName := "workspaceName"
		workspaceResourceGroup := "resourcegroup"
		id := "workspaceID"
		skuName := armoperationalinsights.WorkspaceSKUNameEnumStandalone
		workplacesClientMock.EXPECT().Get(ctx, workspaceResourceGroup, workspaceName, nil).Return(armoperationalinsights.WorkspacesClientGetResponse{}, errors.New("not found"))
		workplacesClientMock.EXPECT().BeginCreateOrUpdate(ctx, workspaceResourceGroup, workspaceName,
			armoperationalinsights.Workspace{
				Location: to.Ptr("eastus"),
				Properties: &armoperationalinsights.WorkspaceProperties{
					SKU: &armoperationalinsights.WorkspaceSKU{
						Name: &skuName,
					},
				},
			},
			nil,
		).Return(pollerMock, nil)
		pollerMock.EXPECT().PollUntilDone(ctx, nil).Return(armoperationalinsights.WorkspacesClientCreateOrUpdateResponse{
			Workspace: armoperationalinsights.Workspace{
				ID: to.Ptr(id),
			},
		}, nil)

		workspaceID, err := CheckLogAnalyticsWorkspaceForMonitoring(ctx,
			workplacesClientMock,
			"eastus", workspaceResourceGroup, "", workspaceName)

		Expect(err).NotTo(HaveOccurred())
		Expect(workspaceID).To(Equal(id))
	})

	It("should fail if CreateOrUpdate returns error", func() {
		workplacesClientMock.EXPECT().Get(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(armoperationalinsights.WorkspacesClientGetResponse{}, errors.New("not found"))
		workplacesClientMock.EXPECT().BeginCreateOrUpdate(ctx, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(pollerMock, errors.New("error"))

		_, err := CheckLogAnalyticsWorkspaceForMonitoring(ctx,
			workplacesClientMock,
			"eastus", "workspaceResourceGroup", "", "workspaceName")

		Expect(err).To(HaveOccurred())
	})

	It("should fail if PollUntilDone returns error", func() {
		workplacesClientMock.EXPECT().Get(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(armoperationalinsights.WorkspacesClientGetResponse{}, errors.New("not found"))
		workplacesClientMock.EXPECT().BeginCreateOrUpdate(ctx, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(pollerMock, nil)
		pollerMock.EXPECT().PollUntilDone(ctx, nil).Return(armoperationalinsights.WorkspacesClientCreateOrUpdateResponse{}, errors.New("error"))

		_, err := CheckLogAnalyticsWorkspaceForMonitoring(ctx,
			workplacesClientMock,
			"eastus", "workspaceResourceGroup", "", "workspaceName")

		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("CheckAvailabilityZonesSupport", func() {
	var (
		locationWithAZ    = "eastus"
		locationWithoutAZ = "westus"
	)

	It("should success for region with availability zones", func() {
		result := CheckAvailabilityZonesSupport(locationWithAZ)
		Expect(result).To(BeTrue())
	})

	It("should fail for region with availability zones", func() {
		result := CheckAvailabilityZonesSupport(locationWithoutAZ)
		Expect(result).To(BeFalse())
	})
})
