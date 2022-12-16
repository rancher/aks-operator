package aks

import (
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/services/operationalinsights/mgmt/2020-08-01/operationalinsights"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher/aks-operator/pkg/aks/services/mock_services"
	"github.com/stretchr/testify/assert"
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

var _ = Describe("checkLogAnalyticsWorkspaceForMonitoring", func() {
	var (
		workplacesClientMock *mock_services.MockWorkplacesClientInterface
		mockCtrl             *gomock.Controller
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		workplacesClientMock = mock_services.NewMockWorkplacesClientInterface(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("should return workspaceID if workspace already exists", func() {
		workspaceName := "workspaceName"
		workspaceResourceGroup := "resourcegroup"
		id := "workspaceID"
		workplacesClientMock.EXPECT().Get(ctx, workspaceResourceGroup, workspaceName).Return(operationalinsights.Workspace{
			ID: &id,
		}, nil)
		workspaceID, err := checkLogAnalyticsWorkspaceForMonitoring(ctx,
			workplacesClientMock,
			"eastus", workspaceResourceGroup, "", workspaceName)
		Expect(err).NotTo(HaveOccurred())
		Expect(workspaceID).To(Equal(id))
	})

	It("should create workspace if it doesn't exist", func() {
		workspaceName := "workspaceName"
		workspaceResourceGroup := "resourcegroup"
		id := "workspaceID"
		workplacesClientMock.EXPECT().Get(ctx, workspaceResourceGroup, workspaceName).Return(operationalinsights.Workspace{}, errors.New("not found"))
		workplacesClientMock.EXPECT().CreateOrUpdate(ctx, workspaceResourceGroup, workspaceName,
			operationalinsights.Workspace{
				Location: to.StringPtr("eastus"),
				WorkspaceProperties: &operationalinsights.WorkspaceProperties{
					Sku: &operationalinsights.WorkspaceSku{
						Name: operationalinsights.WorkspaceSkuNameEnumStandalone,
					},
				},
			},
		).Return(operationalinsights.WorkspacesCreateOrUpdateFuture{}, nil)
		workplacesClientMock.EXPECT().AsyncCreateUpdateResult(gomock.Any()).Return(operationalinsights.Workspace{
			ID: &id,
		}, nil)

		workspaceID, err := checkLogAnalyticsWorkspaceForMonitoring(ctx,
			workplacesClientMock,
			"eastus", workspaceResourceGroup, "", workspaceName)

		Expect(err).NotTo(HaveOccurred())
		Expect(workspaceID).To(Equal(id))
	})

	It("should fail if CreateOrUpdate returns error", func() {
		workplacesClientMock.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(operationalinsights.Workspace{}, errors.New("not found"))
		workplacesClientMock.EXPECT().CreateOrUpdate(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(operationalinsights.WorkspacesCreateOrUpdateFuture{}, errors.New("error"))

		_, err := checkLogAnalyticsWorkspaceForMonitoring(ctx,
			workplacesClientMock,
			"eastus", "workspaceResourceGroup", "", "workspaceName")

		Expect(err).To(HaveOccurred())
	})

	It("should fail if AsyncCreateUpdateResult returns error", func() {
		workplacesClientMock.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(operationalinsights.Workspace{}, errors.New("not found"))
		workplacesClientMock.EXPECT().CreateOrUpdate(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(operationalinsights.WorkspacesCreateOrUpdateFuture{}, nil)
		workplacesClientMock.EXPECT().AsyncCreateUpdateResult(gomock.Any()).Return(operationalinsights.Workspace{}, errors.New("error"))

		_, err := checkLogAnalyticsWorkspaceForMonitoring(ctx,
			workplacesClientMock,
			"eastus", "workspaceResourceGroup", "", "workspaceName")

		Expect(err).To(HaveOccurred())
	})
})
