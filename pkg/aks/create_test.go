package aks

import (
	"errors"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/operationalinsights/armoperationalinsights"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher/aks-operator/pkg/aks/services/mock_services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	"github.com/rancher/aks-operator/pkg/utils"
	"go.uber.org/mock/gomock"
)

var _ = Describe("CreateResourceGroup", func() {
	var (
		mockController          *gomock.Controller
		mockResourceGroupClient *mock_services.MockResourceGroupsClientInterface
		resourceGroupName       = "test-rg"
		location                = "eastus"
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		mockResourceGroupClient = mock_services.NewMockResourceGroupsClientInterface(mockController)
	})

	AfterEach(func() {
		mockController.Finish()
	})

	It("should successfully create a resource group", func() {
		mockResourceGroupClient.EXPECT().CreateOrUpdate(ctx, resourceGroupName, armresources.ResourceGroup{
			Name:     to.Ptr(resourceGroupName),
			Location: to.Ptr(location),
		}, nil).Return(armresources.ResourceGroupsClientCreateOrUpdateResponse{}, nil)

		Expect(CreateResourceGroup(ctx, mockResourceGroupClient, &aksv1.AKSClusterConfigSpec{
			ResourceGroup:    resourceGroupName,
			ResourceLocation: location,
		})).To(Succeed())
	})

	It("should catch error when resource group creation fails", func() {
		mockResourceGroupClient.EXPECT().CreateOrUpdate(ctx, resourceGroupName, armresources.ResourceGroup{
			Name:     to.Ptr(resourceGroupName),
			Location: to.Ptr(location),
		}, nil).Return(armresources.ResourceGroupsClientCreateOrUpdateResponse{}, errors.New("failed to create resource group"))

		err := CreateResourceGroup(ctx, mockResourceGroupClient, &aksv1.AKSClusterConfigSpec{
			ResourceGroup:    resourceGroupName,
			ResourceLocation: location,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create resource group"))
	})
})

var _ = Describe("newManagedCluster", func() {
	var (
		mockController       *gomock.Controller
		workplacesClientMock *mock_services.MockWorkplacesClientInterface
		clusterSpec          *aksv1.AKSClusterConfigSpec
		cred                 *Credentials
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		workplacesClientMock = mock_services.NewMockWorkplacesClientInterface(mockController)
		clusterSpec = newTestClusterSpec()
		clusterSpec.Monitoring = to.Ptr(true)
		cred = &Credentials{
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
			TenantID:     "test-tenant-id",
		}
	})

	AfterEach(func() {
		mockController.Finish()
	})

	It("should successfully create a managed cluster", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)

		clusterSpec.LoadBalancerSKU = to.Ptr("standard")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.Tags).To(HaveKeyWithValue("test-tag", to.Ptr("test-value")))
		Expect(managedCluster.Tags).To(HaveKeyWithValue("empty-tag", to.Ptr("")))
		Expect(*managedCluster.Properties.NetworkProfile.NetworkPolicy).To(Equal(armcontainerservice.NetworkPolicy(String(clusterSpec.NetworkPolicy))))
		Expect(*managedCluster.Properties.NetworkProfile.LoadBalancerSKU).To(Equal(armcontainerservice.LoadBalancerSKU(String(clusterSpec.LoadBalancerSKU))))
		Expect(*managedCluster.Properties.NetworkProfile.NetworkPlugin).To(Equal(armcontainerservice.NetworkPlugin(String(clusterSpec.NetworkPlugin))))
		Expect(managedCluster.Properties.NetworkProfile.DNSServiceIP).To(Equal(clusterSpec.NetworkDNSServiceIP))
		Expect(managedCluster.Properties.NetworkProfile.ServiceCidr).To(Equal(clusterSpec.NetworkServiceCIDR))
		Expect(managedCluster.Properties.NetworkProfile.PodCidr).To(Equal(clusterSpec.NetworkPodCIDR))
		Expect(*managedCluster.Properties.NetworkProfile.OutboundType).To(Equal(armcontainerservice.OutboundTypeLoadBalancer))
		agentPoolProfiles := managedCluster.Properties.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(agentPoolProfiles[0].Name).To(Equal(clusterSpec.NodePools[0].Name))
		Expect(agentPoolProfiles[0].Count).To(Equal(clusterSpec.NodePools[0].Count))
		Expect(agentPoolProfiles[0].MaxPods).To(Equal(clusterSpec.NodePools[0].MaxPods))
		Expect(agentPoolProfiles[0].OSDiskSizeGB).To(Equal(clusterSpec.NodePools[0].OsDiskSizeGB))
		Expect(*agentPoolProfiles[0].OSDiskType).To(Equal(armcontainerservice.OSDiskType(clusterSpec.NodePools[0].OsDiskType)))
		Expect(*agentPoolProfiles[0].OSType).To(Equal(armcontainerservice.OSType(clusterSpec.NodePools[0].OsType)))
		Expect(*agentPoolProfiles[0].VMSize).To(Equal(clusterSpec.NodePools[0].VMSize))
		Expect(*agentPoolProfiles[0].Mode).To(Equal(armcontainerservice.AgentPoolMode(clusterSpec.NodePools[0].Mode)))
		Expect(agentPoolProfiles[0].OrchestratorVersion).To(Equal(clusterSpec.NodePools[0].OrchestratorVersion))
		expectedAvailabilityZones := agentPoolProfiles[0].AvailabilityZones
		clusterSpecAvailabilityZones := *clusterSpec.NodePools[0].AvailabilityZones
		Expect(expectedAvailabilityZones).To(HaveLen(1))
		Expect(*expectedAvailabilityZones[0]).To(Equal(clusterSpecAvailabilityZones[0]))
		Expect(agentPoolProfiles[0].EnableAutoScaling).To(Equal(clusterSpec.NodePools[0].EnableAutoScaling))
		Expect(agentPoolProfiles[0].MinCount).To(Equal(clusterSpec.NodePools[0].MinCount))
		Expect(agentPoolProfiles[0].MaxCount).To(Equal(clusterSpec.NodePools[0].MaxCount))
		Expect(agentPoolProfiles[0].UpgradeSettings.MaxSurge).To(Equal(clusterSpec.NodePools[0].MaxSurge))
		expectedNodeTaints := agentPoolProfiles[0].NodeTaints
		clusterSpecNodeTaints := *clusterSpec.NodePools[0].NodeTaints
		Expect(expectedNodeTaints).To(HaveLen(1))
		Expect(*expectedNodeTaints[0]).To(Equal(clusterSpecNodeTaints[0]))
		Expect(agentPoolProfiles[0].NodeLabels).To(HaveKeyWithValue("node-label", to.Ptr("test-value")))
		Expect(managedCluster.Properties.LinuxProfile.AdminUsername).To(Equal(clusterSpec.LinuxAdminUsername))
		sshPublicKeys := managedCluster.Properties.LinuxProfile.SSH.PublicKeys
		Expect(sshPublicKeys).To(HaveLen(1))
		Expect(sshPublicKeys[0].KeyData).To(Equal(clusterSpec.LinuxSSHPublicKey))
		Expect(managedCluster.Properties.AddonProfiles).To(HaveKey("httpApplicationRouting"))
		Expect(managedCluster.Properties.AddonProfiles["httpApplicationRouting"].Enabled).To(Equal(clusterSpec.HTTPApplicationRouting))
		Expect(managedCluster.Properties.AddonProfiles).To(HaveKey("omsAgent"))
		Expect(managedCluster.Properties.AddonProfiles["omsAgent"].Enabled).To(Equal(clusterSpec.Monitoring))
		Expect(managedCluster.Properties.AddonProfiles["omsAgent"].Config).To(HaveKeyWithValue("logAnalyticsWorkspaceResourceID", to.Ptr("/test-workspace-id")))
		Expect(managedCluster.Location).To(Equal(to.Ptr(clusterSpec.ResourceLocation)))
		Expect(managedCluster.Properties.KubernetesVersion).To(Equal(clusterSpec.KubernetesVersion))
		Expect(managedCluster.Properties.ServicePrincipalProfile).ToNot(BeNil())
		Expect(managedCluster.Properties.ServicePrincipalProfile.ClientID).To(Equal(to.Ptr(cred.ClientID)))
		Expect(managedCluster.Properties.ServicePrincipalProfile.Secret).To(Equal(to.Ptr(cred.ClientSecret)))
		Expect(managedCluster.Properties.DNSPrefix).To(Equal(clusterSpec.DNSPrefix))
		Expect(managedCluster.Properties.APIServerAccessProfile).ToNot(BeNil())
		Expect(managedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges).ToNot(BeNil())
		ipRanges := managedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges
		clusterSpecIPRanges := *clusterSpec.AuthorizedIPRanges
		Expect(ipRanges).To(HaveLen(1))
		Expect(*ipRanges[0]).To(Equal(clusterSpecIPRanges[0]))
		Expect(managedCluster.Properties.APIServerAccessProfile.EnablePrivateCluster).To(Equal(clusterSpec.PrivateCluster))
		Expect(managedCluster.Identity).ToNot(BeNil())
		Expect(*managedCluster.Identity.Type).To(Equal(armcontainerservice.ResourceIdentityTypeSystemAssigned))
		Expect(managedCluster.Properties.APIServerAccessProfile.PrivateDNSZone).To(Equal(clusterSpec.PrivateDNSZone))
	})

	It("should successfully create managed cluster with custom load balancer sku", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.LoadBalancerSKU = to.Ptr("basic")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		Expect(*managedCluster.Properties.NetworkProfile.LoadBalancerSKU).To(Equal(armcontainerservice.LoadBalancerSKUBasic))
	})

	It("should successfully create managed cluster with outboundtype userdefinedrouting", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.OutboundType = to.Ptr("userdefinedrouting")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		Expect(*managedCluster.Properties.NetworkProfile.OutboundType).To(Equal(armcontainerservice.OutboundTypeUserDefinedRouting))
	})

	It("should successfully create managed cluster with outboundtype UserDefinedRouting", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.OutboundType = to.Ptr("UserDefinedRouting")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		Expect(*managedCluster.Properties.NetworkProfile.OutboundType).To(Equal(armcontainerservice.OutboundTypeUserDefinedRouting))
	})

	It("should successfully create managed cluster with custom network plugin without network profile", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.NetworkPlugin = to.Ptr("kubenet")
		clusterSpec.NetworkPolicy = to.Ptr("calico")
		clusterSpec.NetworkDNSServiceIP = to.Ptr("")
		clusterSpec.NetworkServiceCIDR = to.Ptr("")
		clusterSpec.NetworkPodCIDR = to.Ptr("")

		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		Expect(*managedCluster.Properties.NetworkProfile.NetworkPlugin).To(Equal(armcontainerservice.NetworkPluginKubenet))
		Expect(managedCluster.Properties.NetworkProfile.DNSServiceIP).To(Equal(clusterSpec.NetworkDNSServiceIP))
		Expect(managedCluster.Properties.NetworkProfile.ServiceCidr).To(Equal(clusterSpec.NetworkServiceCIDR))
		Expect(managedCluster.Properties.NetworkProfile.PodCidr).To(Equal(clusterSpec.NetworkPodCIDR))
	})

	It("should successfully create managed cluster with custom network plugin", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.NetworkPlugin = to.Ptr("kubenet")
		clusterSpec.NetworkPolicy = to.Ptr("calico")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		Expect(*managedCluster.Properties.NetworkProfile.NetworkPlugin).To(Equal(armcontainerservice.NetworkPluginKubenet))
		Expect(managedCluster.Properties.NetworkProfile.DNSServiceIP).To(Equal(clusterSpec.NetworkDNSServiceIP))
		Expect(managedCluster.Properties.NetworkProfile.ServiceCidr).To(Equal(clusterSpec.NetworkServiceCIDR))
		Expect(managedCluster.Properties.NetworkProfile.PodCidr).To(Equal(clusterSpec.NetworkPodCIDR))
	})

	It("should successfully create managed cluster with custom virtual network resource group", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.VirtualNetworkResourceGroup = to.Ptr("test-vnet-resource-group")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		agentPoolProfiles := managedCluster.Properties.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(String(agentPoolProfiles[0].VnetSubnetID)).To(ContainSubstring(String(clusterSpec.VirtualNetworkResourceGroup)))
	})

	It("should successfully create managed cluster with orchestrator version", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.NodePools[0].OrchestratorVersion = to.Ptr("custom-orchestrator-version")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		agentPoolProfiles := managedCluster.Properties.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(String(agentPoolProfiles[0].OrchestratorVersion)).To(ContainSubstring(String(clusterSpec.NodePools[0].OrchestratorVersion)))
	})

	It("should successfully create managed cluster with no availability zones set", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.NodePools[0].AvailabilityZones = nil
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		agentPoolProfiles := managedCluster.Properties.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(agentPoolProfiles[0].AvailabilityZones).To(BeNil())
	})

	It("should successfully create managed cluster with no autoscaling enabled", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.NodePools[0].EnableAutoScaling = nil
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		agentPoolProfiles := managedCluster.Properties.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(agentPoolProfiles[0].EnableAutoScaling).To(BeNil())
		Expect(agentPoolProfiles[0].MaxCount).To(BeNil())
		Expect(agentPoolProfiles[0].MinCount).To(BeNil())
	})

	It("should successfully create managed cluster with no custom virtual network", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.VirtualNetwork = nil
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		agentPoolProfiles := managedCluster.Properties.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(agentPoolProfiles[0].VnetSubnetID).To(BeNil())
	})

	It("should successfully create managed cluster with no linux profile", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.LinuxAdminUsername = nil
		clusterSpec.LinuxSSHPublicKey = nil
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.Properties.LinuxProfile).To(BeNil())
	})

	It("should successfully create managed cluster with no http application routing", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		clusterSpec.ResourceLocation = "chinaeast"
		clusterSpec.NodePools[0].AvailabilityZones = nil
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.Properties.AddonProfiles).ToNot(HaveKey("httpApplicationRouting"))
	})

	It("should successfully create managed cluster with no monitoring enabled", func() {
		workplacesClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(armoperationalinsights.WorkspacesClientGetResponse{}, nil).Times(0)
		clusterSpec.Monitoring = nil
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.Properties.AddonProfiles).ToNot(HaveKey("omsAgent"))
	})

	It("should successfully create managed cluster with monitoring disabled", func() {
		workplacesClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(armoperationalinsights.WorkspacesClientGetResponse{}, nil).Times(0)
		flag := false
		clusterSpec.Monitoring = &flag
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.Properties.AddonProfiles).To(HaveKey("omsAgent"))
		Expect(*managedCluster.Properties.AddonProfiles["omsAgent"].Enabled).To(BeFalse())
	})

	It("should successfully create managed cluster when phase is set to active", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "active")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.Properties.ServicePrincipalProfile).To(BeNil())
	})

	It("should fail if LogAnalyticsWorkspaceForMonitoring returns error", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{}, errors.New("test-error"))

		workplacesClientMock.EXPECT().BeginCreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&runtime.Poller[armoperationalinsights.WorkspacesClientCreateOrUpdateResponse]{}, errors.New("test-error"))

		_, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).To(HaveOccurred())
	})

	It("should fail if network policy is azure and network plugin is kubenet", func() {
		workplacesClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(armoperationalinsights.WorkspacesClientGetResponse{}, nil).Times(0)
		clusterSpec.NetworkPlugin = to.Ptr("kubenet")
		clusterSpec.NetworkPolicy = to.Ptr("azure")
		_, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).To(HaveOccurred())
	})

	It("should fail for region without availibility zones", func() {
		clusterSpec.ResourceLocation = "westus"
		_, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).To(HaveOccurred())
	})

	It("should successfully create managed cluster with custom node resource group name", func() {
		clusterSpec.NodeResourceGroup = to.Ptr("test-node-resource-group-name")
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "active")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.Properties.NodeResourceGroup).To(Equal(to.Ptr("test-node-resource-group-name")))
	})

	It("should successfully create managed cluster with truncated default node resource group name over 80 characters", func() {
		clusterSpec.ClusterName = "this-is-a-cluster-with-a-very-long-name-that-is-over-80-characters"
		defaultResourceGroupName := "MC_" + clusterSpec.ResourceGroup + "_" + clusterSpec.ClusterName + "_" + clusterSpec.ResourceLocation
		truncated := defaultResourceGroupName[:80]
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "active")
		Expect(err).ToNot(HaveOccurred())
		Expect(managedCluster.Properties.NodeResourceGroup).To(Equal(to.Ptr(truncated)))
	})

	It("should successfully create managed cluster with no TenantID provided", func() {
		workplacesClientMock.EXPECT().Get(ctx, String(clusterSpec.LogAnalyticsWorkspaceGroup), String(clusterSpec.LogAnalyticsWorkspaceName), nil).
			Return(armoperationalinsights.WorkspacesClientGetResponse{
				Workspace: armoperationalinsights.Workspace{
					ID: to.Ptr("test-workspace-id"),
				},
			}, nil)
		cred.TenantID = ""
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.Identity).To(BeNil())
	})
})

var _ = Describe("CreateCluster", func() {
	var (
		mockController       *gomock.Controller
		workplacesClientMock *mock_services.MockWorkplacesClientInterface
		clusterClientMock    *mock_services.MockManagedClustersClientInterface
		pollerMock           *mock_services.MockPoller[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse]
		clusterSpec          *aksv1.AKSClusterConfigSpec
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		workplacesClientMock = mock_services.NewMockWorkplacesClientInterface(mockController)
		clusterClientMock = mock_services.NewMockManagedClustersClientInterface(mockController)
		pollerMock = mock_services.NewMockPoller[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse](mockController)
		clusterSpec = newTestClusterSpec()
	})

	AfterEach(func() {
		mockController.Finish()
	})

	It("should successfully create managed cluster", func() {
		clusterClientMock.EXPECT().BeginCreateOrUpdate(
			ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, gomock.Any(), gomock.Any()).Return(pollerMock, nil)
		Expect(CreateCluster(ctx, &Credentials{}, clusterClientMock, workplacesClientMock, clusterSpec, "test-phase")).To(Succeed())
	})

	It("should fail if clusterClient.CreateOrUpdate returns error", func() {
		clusterClientMock.EXPECT().BeginCreateOrUpdate(
			ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, gomock.Any(), gomock.Any()).Return(pollerMock, errors.New("test-error"))
		Expect(CreateCluster(ctx, &Credentials{}, clusterClientMock, workplacesClientMock, clusterSpec, "test-phase")).ToNot(Succeed())
	})
})

var _ = Describe("CreateOrUpdateAgentPool", func() {
	var (
		mockController      *gomock.Controller
		agentPoolClientMock *mock_services.MockAgentPoolsClientInterface
		clusterSpec         *aksv1.AKSClusterConfigSpec
		nodePoolSpec        *aksv1.AKSNodePool
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		agentPoolClientMock = mock_services.NewMockAgentPoolsClientInterface(mockController)
		clusterSpec = newTestClusterSpec()
		nodePoolSpec = &aksv1.AKSNodePool{
			Name:                to.Ptr("test-nodepool"),
			Count:               to.Ptr(int32(1)),
			MaxPods:             to.Ptr(int32(1)),
			OsDiskSizeGB:        to.Ptr(int32(1)),
			OsDiskType:          "Ephemeral",
			OsType:              "Linux",
			VMSize:              "Standard_D2_v2",
			Mode:                "System",
			OrchestratorVersion: to.Ptr("test-version"),
			AvailabilityZones:   to.Ptr([]string{"test-az"}),
			EnableAutoScaling:   to.Ptr(true),
			MinCount:            to.Ptr(int32(1)),
			MaxCount:            to.Ptr(int32(2)),
			MaxSurge:            to.Ptr("10%"),
			NodeTaints:          to.Ptr([]string{"node=taint:NoSchedule"}),
			NodeLabels: map[string]*string{
				"node-label": to.Ptr("test-value"),
			},
		}
	})

	AfterEach(func() {
		mockController.Finish()
	})

	It("should successfully create agent pool", func() {
		agentPoolClientMock.EXPECT().BeginCreateOrUpdate(
			ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, *nodePoolSpec.Name,
			armcontainerservice.AgentPool{
				Properties: &armcontainerservice.ManagedClusterAgentPoolProfileProperties{
					Count:               nodePoolSpec.Count,
					MaxPods:             nodePoolSpec.MaxPods,
					OSDiskSizeGB:        nodePoolSpec.OsDiskSizeGB,
					OSDiskType:          to.Ptr(armcontainerservice.OSDiskType(nodePoolSpec.OsDiskType)),
					OSType:              to.Ptr(armcontainerservice.OSType(nodePoolSpec.OsType)),
					VMSize:              to.Ptr(nodePoolSpec.VMSize),
					Mode:                to.Ptr(armcontainerservice.AgentPoolMode(nodePoolSpec.Mode)),
					Type:                to.Ptr(armcontainerservice.AgentPoolTypeVirtualMachineScaleSets),
					OrchestratorVersion: nodePoolSpec.OrchestratorVersion,
					AvailabilityZones:   utils.ConvertToSliceOfPointers(nodePoolSpec.AvailabilityZones),
					EnableAutoScaling:   nodePoolSpec.EnableAutoScaling,
					MinCount:            nodePoolSpec.MinCount,
					MaxCount:            nodePoolSpec.MaxCount,
					NodeTaints:          utils.ConvertToSliceOfPointers(nodePoolSpec.NodeTaints),
					NodeLabels:          nodePoolSpec.NodeLabels,
					UpgradeSettings: &armcontainerservice.AgentPoolUpgradeSettings{
						MaxSurge: nodePoolSpec.MaxSurge,
					},
				},
			}).Return(&runtime.Poller[armcontainerservice.AgentPoolsClientCreateOrUpdateResponse]{}, nil)
		Expect(CreateOrUpdateAgentPool(ctx, agentPoolClientMock, clusterSpec, nodePoolSpec)).To(Succeed())
	})

	It("should fail if agentPoolClient.CreateOrUpdate returns error", func() {
		agentPoolClientMock.EXPECT().BeginCreateOrUpdate(
			ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, String(nodePoolSpec.Name), gomock.Any()).
			Return(&runtime.Poller[armcontainerservice.AgentPoolsClientCreateOrUpdateResponse]{}, errors.New("test-error"))

		Expect(CreateOrUpdateAgentPool(ctx, agentPoolClientMock, clusterSpec, nodePoolSpec)).ToNot(Succeed())
	})

	It("should fail for region without avaibility zones", func() {
		clusterSpec.ResourceLocation = "westus"
		Expect(CreateOrUpdateAgentPool(ctx, agentPoolClientMock, clusterSpec, nodePoolSpec)).ToNot(Succeed())
	})
})

func newTestClusterSpec() *aksv1.AKSClusterConfigSpec {
	return &aksv1.AKSClusterConfigSpec{
		ResourceLocation: "eastus",
		Tags: map[string]string{
			"test-tag":  "test-value",
			"empty-tag": "",
		},
		NetworkPolicy:       to.Ptr("azure"),
		NetworkPlugin:       to.Ptr("azure"),
		NetworkDNSServiceIP: to.Ptr("test-dns-service-ip"),
		NetworkServiceCIDR:  to.Ptr("test-service-cidr"),
		NetworkPodCIDR:      to.Ptr("test-pod-cidr"),
		ResourceGroup:       "test-rg",
		VirtualNetwork:      to.Ptr("test-virtual-network"),
		Subnet:              to.Ptr("test-subnet"),
		NodePools: []aksv1.AKSNodePool{
			{
				Name:                to.Ptr("test-node-pool"),
				Count:               to.Ptr(int32(1)),
				MaxPods:             to.Ptr(int32(1)),
				OsDiskSizeGB:        to.Ptr(int32(1)),
				OsDiskType:          "Ephemeral",
				OsType:              "Linux",
				VMSize:              "Standard_D2_v2",
				Mode:                "System",
				OrchestratorVersion: to.Ptr("test-orchestrator-version"),
				AvailabilityZones:   to.Ptr([]string{"test-availability-zone"}),
				EnableAutoScaling:   to.Ptr(true),
				MinCount:            to.Ptr(int32(1)),
				MaxCount:            to.Ptr(int32(2)),
				MaxSurge:            to.Ptr("10%"),
				NodeTaints:          to.Ptr([]string{"node=taint:NoSchedule"}),
				NodeLabels: map[string]*string{
					"node-label": to.Ptr("test-value"),
				},
			},
		},
		LinuxAdminUsername:         to.Ptr("test-admin-username"),
		LinuxSSHPublicKey:          to.Ptr("test-ssh-public-key"),
		HTTPApplicationRouting:     to.Ptr(true),
		Monitoring:                 to.Ptr(false),
		KubernetesVersion:          to.Ptr("test-kubernetes-version"),
		DNSPrefix:                  to.Ptr("test-dns-prefix"),
		AuthorizedIPRanges:         to.Ptr([]string{"test-authorized-ip-range"}),
		PrivateCluster:             to.Ptr(true),
		PrivateDNSZone:             to.Ptr("test-private-dns-zone"),
		LogAnalyticsWorkspaceGroup: to.Ptr("test-log-analytics-workspace-group"),
		LogAnalyticsWorkspaceName:  to.Ptr("test-log-analytics-workspace-name"),
	}
}
