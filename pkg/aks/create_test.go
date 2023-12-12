package aks

import (
	"errors"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/Azure/azure-sdk-for-go/services/operationalinsights/mgmt/2020-08-01/operationalinsights"
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
		mockResourceGroupClient.EXPECT().CreateOrUpdate(ctx, resourceGroupName, resources.Group{
			Name:     to.StringPtr(resourceGroupName),
			Location: to.StringPtr(location),
		}).Return(resources.Group{}, nil)

		Expect(CreateResourceGroup(ctx, mockResourceGroupClient, &aksv1.AKSClusterConfigSpec{
			ResourceGroup:    resourceGroupName,
			ResourceLocation: location,
		})).To(Succeed())
	})

	It("should catch error when resource group creation fails", func() {
		mockResourceGroupClient.EXPECT().CreateOrUpdate(ctx, resourceGroupName, resources.Group{
			Name:     to.StringPtr(resourceGroupName),
			Location: to.StringPtr(location),
		}).Return(resources.Group{}, errors.New("failed to create resource group"))

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
		clusterSpec.Monitoring = to.BoolPtr(true)
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
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)

		clusterSpec.LoadBalancerSKU = to.StringPtr("standard")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.Tags).To(HaveKeyWithValue("test-tag", to.StringPtr("test-value")))
		Expect(managedCluster.NetworkProfile.NetworkPolicy).To(Equal(containerservice.NetworkPolicy(to.String(clusterSpec.NetworkPolicy))))
		Expect(managedCluster.NetworkProfile.LoadBalancerSku).To(Equal(containerservice.LoadBalancerSku(to.String(clusterSpec.LoadBalancerSKU))))
		Expect(managedCluster.NetworkProfile.NetworkPlugin).To(Equal(containerservice.NetworkPlugin(to.String(clusterSpec.NetworkPlugin))))
		Expect(managedCluster.NetworkProfile.DNSServiceIP).To(Equal(clusterSpec.NetworkDNSServiceIP))
		Expect(managedCluster.NetworkProfile.ServiceCidr).To(Equal(clusterSpec.NetworkServiceCIDR))
		Expect(managedCluster.NetworkProfile.PodCidr).To(Equal(clusterSpec.NetworkPodCIDR))
		Expect(managedCluster.NetworkProfile.OutboundType).To(Equal(containerservice.LoadBalancer))
		agentPoolProfiles := *managedCluster.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(agentPoolProfiles[0].Name).To(Equal(clusterSpec.NodePools[0].Name))
		Expect(agentPoolProfiles[0].Count).To(Equal(clusterSpec.NodePools[0].Count))
		Expect(agentPoolProfiles[0].MaxPods).To(Equal(clusterSpec.NodePools[0].MaxPods))
		Expect(agentPoolProfiles[0].OsDiskSizeGB).To(Equal(clusterSpec.NodePools[0].OsDiskSizeGB))
		Expect(agentPoolProfiles[0].OsDiskType).To(Equal(containerservice.OSDiskType(clusterSpec.NodePools[0].OsDiskType)))
		Expect(agentPoolProfiles[0].OsType).To(Equal(containerservice.OSType(clusterSpec.NodePools[0].OsType)))
		Expect(agentPoolProfiles[0].VMSize).To(Equal(containerservice.VMSizeTypes(clusterSpec.NodePools[0].VMSize)))
		Expect(agentPoolProfiles[0].Mode).To(Equal(containerservice.AgentPoolMode(clusterSpec.NodePools[0].Mode)))
		Expect(agentPoolProfiles[0].OrchestratorVersion).To(Equal(clusterSpec.NodePools[0].OrchestratorVersion))
		expectedAvailabilityZones := *agentPoolProfiles[0].AvailabilityZones
		clusterSpecAvailabilityZones := *clusterSpec.NodePools[0].AvailabilityZones
		Expect(expectedAvailabilityZones).To(HaveLen(1))
		Expect(expectedAvailabilityZones[0]).To(Equal(clusterSpecAvailabilityZones[0]))
		Expect(agentPoolProfiles[0].EnableAutoScaling).To(Equal(clusterSpec.NodePools[0].EnableAutoScaling))
		Expect(agentPoolProfiles[0].MinCount).To(Equal(clusterSpec.NodePools[0].MinCount))
		Expect(agentPoolProfiles[0].MaxCount).To(Equal(clusterSpec.NodePools[0].MaxCount))
		Expect(agentPoolProfiles[0].UpgradeSettings.MaxSurge).To(Equal(clusterSpec.NodePools[0].MaxSurge))
		expectedNodeTaints := *agentPoolProfiles[0].NodeTaints
		clusterSpecNodeTaints := *clusterSpec.NodePools[0].NodeTaints
		Expect(expectedNodeTaints).To(HaveLen(1))
		Expect(expectedNodeTaints[0]).To(Equal(clusterSpecNodeTaints[0]))
		Expect(agentPoolProfiles[0].NodeLabels).To(HaveKeyWithValue("node-label", to.StringPtr("test-value")))
		Expect(managedCluster.LinuxProfile.AdminUsername).To(Equal(clusterSpec.LinuxAdminUsername))
		sshPublicKeys := *managedCluster.LinuxProfile.SSH.PublicKeys
		Expect(sshPublicKeys).To(HaveLen(1))
		Expect(sshPublicKeys[0].KeyData).To(Equal(clusterSpec.LinuxSSHPublicKey))
		Expect(managedCluster.AddonProfiles).To(HaveKey("httpApplicationRouting"))
		Expect(managedCluster.AddonProfiles["httpApplicationRouting"].Enabled).To(Equal(clusterSpec.HTTPApplicationRouting))
		Expect(managedCluster.AddonProfiles).To(HaveKey("omsAgent"))
		Expect(managedCluster.AddonProfiles["omsAgent"].Enabled).To(Equal(clusterSpec.Monitoring))
		Expect(managedCluster.AddonProfiles["omsAgent"].Config).To(HaveKeyWithValue("logAnalyticsWorkspaceResourceID", to.StringPtr("/test-workspace-id")))
		Expect(managedCluster.Location).To(Equal(to.StringPtr(clusterSpec.ResourceLocation)))
		Expect(managedCluster.KubernetesVersion).To(Equal(clusterSpec.KubernetesVersion))
		Expect(managedCluster.ServicePrincipalProfile).ToNot(BeNil())
		Expect(managedCluster.ServicePrincipalProfile.ClientID).To(Equal(to.StringPtr(cred.ClientID)))
		Expect(managedCluster.ServicePrincipalProfile.Secret).To(Equal(to.StringPtr(cred.ClientSecret)))
		Expect(managedCluster.DNSPrefix).To(Equal(clusterSpec.DNSPrefix))
		Expect(managedCluster.APIServerAccessProfile).ToNot(BeNil())
		Expect(managedCluster.APIServerAccessProfile.AuthorizedIPRanges).ToNot(BeNil())
		ipRanges := *managedCluster.APIServerAccessProfile.AuthorizedIPRanges
		clusterSpecIPRanges := *clusterSpec.AuthorizedIPRanges
		Expect(ipRanges).To(HaveLen(1))
		Expect(ipRanges[0]).To(Equal(clusterSpecIPRanges[0]))
		Expect(managedCluster.APIServerAccessProfile.EnablePrivateCluster).To(Equal(clusterSpec.PrivateCluster))
		Expect(managedCluster.Identity).ToNot(BeNil())
		Expect(managedCluster.Identity.Type).To(Equal(containerservice.ResourceIdentityTypeSystemAssigned))
		Expect(managedCluster.Identity.TenantID).To(Equal(to.StringPtr(cred.TenantID)))
		Expect(managedCluster.APIServerAccessProfile.PrivateDNSZone).To(Equal(clusterSpec.PrivateDNSZone))
	})

	It("should successfully create managed cluster with custom load balancer sku", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		clusterSpec.LoadBalancerSKU = to.StringPtr("basic")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		Expect(managedCluster.NetworkProfile.LoadBalancerSku).To(Equal(containerservice.Basic))
	})

	It("should successfully create managed cluster with outboundtype userdefinedrouting", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		clusterSpec.OutboundType = to.StringPtr("userDefinedRouting")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		Expect(managedCluster.NetworkProfile.OutboundType).To(Equal(containerservice.UserDefinedRouting))
	})

	It("should successfully create managed cluster with custom network plugin without network profile", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		clusterSpec.NetworkPlugin = to.StringPtr("kubenet")
		clusterSpec.NetworkPolicy = to.StringPtr("calico")
		clusterSpec.NetworkDNSServiceIP = to.StringPtr("")
		clusterSpec.NetworkServiceCIDR = to.StringPtr("")
		clusterSpec.NetworkPodCIDR = to.StringPtr("")

		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		Expect(managedCluster.NetworkProfile.NetworkPlugin).To(Equal(containerservice.Kubenet))
		Expect(managedCluster.NetworkProfile.DNSServiceIP).To(Equal(clusterSpec.NetworkDNSServiceIP))
		Expect(managedCluster.NetworkProfile.ServiceCidr).To(Equal(clusterSpec.NetworkServiceCIDR))
		Expect(managedCluster.NetworkProfile.PodCidr).To(Equal(clusterSpec.NetworkPodCIDR))
	})

	It("should successfully create managed cluster with custom network plugin", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		clusterSpec.NetworkPlugin = to.StringPtr("kubenet")
		clusterSpec.NetworkPolicy = to.StringPtr("calico")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		Expect(managedCluster.NetworkProfile.NetworkPlugin).To(Equal(containerservice.Kubenet))
		Expect(managedCluster.NetworkProfile.DNSServiceIP).To(Equal(clusterSpec.NetworkDNSServiceIP))
		Expect(managedCluster.NetworkProfile.ServiceCidr).To(Equal(clusterSpec.NetworkServiceCIDR))
		Expect(managedCluster.NetworkProfile.PodCidr).To(Equal(clusterSpec.NetworkPodCIDR))
	})

	It("should successfully create managed cluster with custom virtual network resource group", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		clusterSpec.VirtualNetworkResourceGroup = to.StringPtr("test-vnet-resource-group")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		agentPoolProfiles := *managedCluster.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(to.String(agentPoolProfiles[0].VnetSubnetID)).To(ContainSubstring(to.String(clusterSpec.VirtualNetworkResourceGroup)))
	})

	It("should successfully create managed cluster with orchestrator version", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		clusterSpec.NodePools[0].OrchestratorVersion = to.StringPtr("custom-orchestrator-version")
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		agentPoolProfiles := *managedCluster.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(to.String(agentPoolProfiles[0].OrchestratorVersion)).To(ContainSubstring(to.String(clusterSpec.NodePools[0].OrchestratorVersion)))
	})

	It("should successfully create managed cluster with no availability zones set", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		clusterSpec.NodePools[0].AvailabilityZones = nil
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		agentPoolProfiles := *managedCluster.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(agentPoolProfiles[0].AvailabilityZones).To(BeNil())
	})

	It("should successfully create managed cluster with no autoscaling enabled", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		clusterSpec.NodePools[0].EnableAutoScaling = nil
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())
		agentPoolProfiles := *managedCluster.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(agentPoolProfiles[0].EnableAutoScaling).To(BeNil())
		Expect(agentPoolProfiles[0].MaxCount).To(BeNil())
		Expect(agentPoolProfiles[0].MinCount).To(BeNil())
	})

	It("should successfully create managed cluster with no custom virtual network", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		clusterSpec.VirtualNetwork = nil
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		agentPoolProfiles := *managedCluster.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(agentPoolProfiles[0].VnetSubnetID).To(BeNil())
	})

	It("should successfully create managed cluster with no linux profile", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		clusterSpec.LinuxAdminUsername = nil
		clusterSpec.LinuxSSHPublicKey = nil
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.LinuxProfile).To(BeNil())
	})

	It("should successfully create managed cluster with no http application routing", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		clusterSpec.ResourceLocation = "chinaeast"
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.AddonProfiles).ToNot(HaveKey("httpApplicationRouting"))
	})

	It("should successfully create managed cluster with no monitoring enabled", func() {
		workplacesClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(operationalinsights.Workspace{}, nil).Times(0)
		clusterSpec.Monitoring = nil
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.AddonProfiles).ToNot(HaveKey("omsagent"))
	})

	It("should successfully create managed cluster when phase is set to active", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "active")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.ServicePrincipalProfile).To(BeNil())
	})

	It("should fail if LogAnalyticsWorkspaceForMonitoring returns error", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{}, errors.New("test-error"))

		workplacesClientMock.EXPECT().CreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(operationalinsights.WorkspacesCreateOrUpdateFuture{}, errors.New("test-error"))

		_, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).To(HaveOccurred())
	})

	It("should fail if network policy is azure and network plugin is kubenet", func() {
		workplacesClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(operationalinsights.Workspace{}, nil).Times(0)
		clusterSpec.NetworkPlugin = to.StringPtr("kubenet")
		clusterSpec.NetworkPolicy = to.StringPtr("azure")
		_, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "test-phase")
		Expect(err).To(HaveOccurred())
	})

	It("should successfully create managed cluster with custom node resource group name", func() {
		clusterSpec.NodeResourceGroup = to.StringPtr("test-node-resource-group-name")
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "active")
		Expect(err).ToNot(HaveOccurred())

		Expect(managedCluster.ManagedClusterProperties.NodeResourceGroup).To(Equal(to.StringPtr("test-node-resource-group-name")))
	})

	It("should successfully create managed cluster with truncated default node resource group name over 80 characters", func() {
		clusterSpec.ClusterName = "this-is-a-cluster-with-a-very-long-name-that-is-over-80-characters"
		defaultResourceGroupName := "MC_" + clusterSpec.ResourceGroup + "_" + clusterSpec.ClusterName + "_" + clusterSpec.ResourceLocation
		truncated := defaultResourceGroupName[:80]
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
			}, nil)
		managedCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "active")
		Expect(err).ToNot(HaveOccurred())
		Expect(managedCluster.ManagedClusterProperties.NodeResourceGroup).To(Equal(to.StringPtr(truncated)))
	})

	It("should successfully create managed cluster with no TenantID provided", func() {
		workplacesClientMock.EXPECT().Get(ctx, to.String(clusterSpec.LogAnalyticsWorkspaceGroup), to.String(clusterSpec.LogAnalyticsWorkspaceName)).
			Return(operationalinsights.Workspace{
				ID: to.StringPtr("test-workspace-id"),
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
		clusterSpec          *aksv1.AKSClusterConfigSpec
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		workplacesClientMock = mock_services.NewMockWorkplacesClientInterface(mockController)
		clusterClientMock = mock_services.NewMockManagedClustersClientInterface(mockController)
		clusterSpec = newTestClusterSpec()
	})

	AfterEach(func() {
		mockController.Finish()
	})

	It("should successfully create managed cluster", func() {
		clusterClientMock.EXPECT().CreateOrUpdate(
			ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, gomock.Any()).Return(containerservice.ManagedClustersCreateOrUpdateFuture{}, nil)
		Expect(CreateCluster(ctx, &Credentials{}, clusterClientMock, workplacesClientMock, clusterSpec, "test-phase")).To(Succeed())
	})

	It("should fail if clusterClient.CreateOrUpdate returns error", func() {
		clusterClientMock.EXPECT().CreateOrUpdate(
			ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, gomock.Any()).Return(containerservice.ManagedClustersCreateOrUpdateFuture{}, errors.New("test-error"))
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
			Name:                to.StringPtr("test-nodepool"),
			Count:               to.Int32Ptr(1),
			MaxPods:             to.Int32Ptr(1),
			OsDiskSizeGB:        to.Int32Ptr(1),
			OsDiskType:          "Ephemeral",
			OsType:              "Linux",
			VMSize:              "Standard_D2_v2",
			Mode:                "System",
			OrchestratorVersion: to.StringPtr("test-version"),
			AvailabilityZones:   to.StringSlicePtr([]string{"test-az"}),
			EnableAutoScaling:   to.BoolPtr(true),
			MinCount:            to.Int32Ptr(1),
			MaxCount:            to.Int32Ptr(2),
			MaxSurge:            to.StringPtr("10%"),
			NodeTaints:          to.StringSlicePtr([]string{"node=taint:NoSchedule"}),
			NodeLabels: map[string]*string{
				"node-label": to.StringPtr("test-value"),
			},
		}
	})

	AfterEach(func() {
		mockController.Finish()
	})

	It("should successfully create agent pool", func() {
		agentPoolClientMock.EXPECT().CreateOrUpdate(
			ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, to.String(nodePoolSpec.Name),
			containerservice.AgentPool{
				ManagedClusterAgentPoolProfileProperties: &containerservice.ManagedClusterAgentPoolProfileProperties{
					Count:               nodePoolSpec.Count,
					MaxPods:             nodePoolSpec.MaxPods,
					OsDiskSizeGB:        nodePoolSpec.OsDiskSizeGB,
					OsDiskType:          containerservice.OSDiskType(nodePoolSpec.OsDiskType),
					OsType:              containerservice.OSType(nodePoolSpec.OsType),
					VMSize:              containerservice.VMSizeTypes(nodePoolSpec.VMSize),
					Mode:                containerservice.AgentPoolMode(nodePoolSpec.Mode),
					Type:                containerservice.VirtualMachineScaleSets,
					OrchestratorVersion: nodePoolSpec.OrchestratorVersion,
					AvailabilityZones:   nodePoolSpec.AvailabilityZones,
					EnableAutoScaling:   nodePoolSpec.EnableAutoScaling,
					MinCount:            nodePoolSpec.MinCount,
					MaxCount:            nodePoolSpec.MaxCount,
					NodeTaints:          nodePoolSpec.NodeTaints,
					NodeLabels:          nodePoolSpec.NodeLabels,
					UpgradeSettings: &containerservice.AgentPoolUpgradeSettings{
						MaxSurge: nodePoolSpec.MaxSurge,
					},
				},
			}).Return(containerservice.AgentPoolsCreateOrUpdateFuture{}, nil)
		Expect(CreateOrUpdateAgentPool(ctx, agentPoolClientMock, clusterSpec, nodePoolSpec)).To(Succeed())
	})

	It("should fail if agentPoolClient.CreateOrUpdate returns error", func() {
		agentPoolClientMock.EXPECT().CreateOrUpdate(
			ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, to.String(nodePoolSpec.Name), gomock.Any()).
			Return(containerservice.AgentPoolsCreateOrUpdateFuture{}, errors.New("test-error"))

		Expect(CreateOrUpdateAgentPool(ctx, agentPoolClientMock, clusterSpec, nodePoolSpec)).ToNot(Succeed())
	})
})

func newTestClusterSpec() *aksv1.AKSClusterConfigSpec {
	return &aksv1.AKSClusterConfigSpec{
		ResourceLocation: "eastus",
		Tags: map[string]string{
			"test-tag": "test-value",
		},
		NetworkPolicy:       to.StringPtr("azure"),
		NetworkPlugin:       to.StringPtr("azure"),
		NetworkDNSServiceIP: to.StringPtr("test-dns-service-ip"),
		NetworkServiceCIDR:  to.StringPtr("test-service-cidr"),
		NetworkPodCIDR:      to.StringPtr("test-pod-cidr"),
		ResourceGroup:       "test-rg",
		VirtualNetwork:      to.StringPtr("test-virtual-network"),
		Subnet:              to.StringPtr("test-subnet"),
		NodePools: []aksv1.AKSNodePool{
			{
				Name:                to.StringPtr("test-node-pool"),
				Count:               to.Int32Ptr(1),
				MaxPods:             to.Int32Ptr(1),
				OsDiskSizeGB:        to.Int32Ptr(1),
				OsDiskType:          "Ephemeral",
				OsType:              "Linux",
				VMSize:              "Standard_D2_v2",
				Mode:                "System",
				OrchestratorVersion: to.StringPtr("test-orchestrator-version"),
				AvailabilityZones:   to.StringSlicePtr([]string{"test-availability-zone"}),
				EnableAutoScaling:   to.BoolPtr(true),
				MinCount:            to.Int32Ptr(1),
				MaxCount:            to.Int32Ptr(2),
				MaxSurge:            to.StringPtr("10%"),
				NodeTaints:          to.StringSlicePtr([]string{"node=taint:NoSchedule"}),
				NodeLabels: map[string]*string{
					"node-label": to.StringPtr("test-value"),
				},
			},
		},
		LinuxAdminUsername:         to.StringPtr("test-admin-username"),
		LinuxSSHPublicKey:          to.StringPtr("test-ssh-public-key"),
		HTTPApplicationRouting:     to.BoolPtr(true),
		Monitoring:                 to.BoolPtr(false),
		KubernetesVersion:          to.StringPtr("test-kubernetes-version"),
		DNSPrefix:                  to.StringPtr("test-dns-prefix"),
		AuthorizedIPRanges:         to.StringSlicePtr([]string{"test-authorized-ip-range"}),
		PrivateCluster:             to.BoolPtr(true),
		PrivateDNSZone:             to.StringPtr("test-private-dns-zone"),
		LogAnalyticsWorkspaceGroup: to.StringPtr("test-log-analytics-workspace-group"),
		LogAnalyticsWorkspaceName:  to.StringPtr("test-log-analytics-workspace-name"),
	}
}
