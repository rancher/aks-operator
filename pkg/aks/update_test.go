package aks

import (
	"errors"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher/aks-operator/pkg/aks/services/mock_services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	"go.uber.org/mock/gomock"
)

var _ = Describe("updateCluster", func() {
	var (
		mockController       *gomock.Controller
		workplacesClientMock *mock_services.MockWorkplacesClientInterface
		clusterSpec          *aksv1.AKSClusterConfigSpec
		cred                 *Credentials
		actualCluster        *armcontainerservice.ManagedCluster
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		workplacesClientMock = mock_services.NewMockWorkplacesClientInterface(mockController)
		clusterSpec = &aksv1.AKSClusterConfigSpec{
			ResourceGroup:     "test-rg",
			ClusterName:       "test-cluster",
			KubernetesVersion: to.Ptr("test-version"),
			NodePools: []aksv1.AKSNodePool{
				{
					Name:       to.Ptr("test-nodepool"),
					MaxSurge:   to.Ptr("13%"),
					NodeTaints: to.Ptr([]string{"node=taint:NoSchedule"}),
					NodeLabels: map[string]*string{
						"node-label": to.Ptr("test-value"),
					},
				},
			},
			AuthorizedIPRanges:  to.Ptr([]string{"test-ip-range"}),
			LinuxAdminUsername:  to.Ptr("test-admin-username"),
			LinuxSSHPublicKey:   to.Ptr("test-ssh-public-key"),
			NetworkPlugin:       to.Ptr("azure"),
			NetworkPolicy:       to.Ptr("azure"),
			NetworkDNSServiceIP: to.Ptr("test-dns-service-ip"),
			NetworkPodCIDR:      to.Ptr("test-pod-cidr"),
			NetworkServiceCIDR:  to.Ptr("test-service-cidr"),
			LoadBalancerSKU:     to.Ptr("standard"),
			Tags: map[string]string{
				"test-tag": "test-value",
			},
		}
		cred = &Credentials{
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
		}
		actualCluster = &armcontainerservice.ManagedCluster{
			Properties: &armcontainerservice.ManagedClusterProperties{
				AddonProfiles: map[string]*armcontainerservice.ManagedClusterAddonProfile{
					"test-addon": {},
				},
			},
		}
	})

	AfterEach(func() {
		mockController.Finish()
	})

	It("should successfully update cluster", func() {
		desiredCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "phase")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := updateCluster(*desiredCluster, *actualCluster, false)
		Expect(updatedCluster.Properties.KubernetesVersion).To(Equal(clusterSpec.KubernetesVersion))
		Expect(updatedCluster.Properties.AddonProfiles).To(HaveKey("test-addon"))
		Expect(updatedCluster.Properties.AddonProfiles).To(HaveKey("httpApplicationRouting"))
		agentPoolProfiles := updatedCluster.Properties.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
		Expect(agentPoolProfiles[0].Name).To(Equal(clusterSpec.NodePools[0].Name))
		Expect(agentPoolProfiles[0].OrchestratorVersion).To(Equal(clusterSpec.KubernetesVersion))
		Expect(agentPoolProfiles[0].UpgradeSettings.MaxSurge).To(Equal(clusterSpec.NodePools[0].MaxSurge))
		expectedNodeTaints := agentPoolProfiles[0].NodeTaints
		clusterSpecNodeTaints := *clusterSpec.NodePools[0].NodeTaints
		Expect(expectedNodeTaints).To(HaveLen(1))
		Expect(*expectedNodeTaints[0]).To(Equal(clusterSpecNodeTaints[0]))
		Expect(agentPoolProfiles[0].NodeLabels).To(HaveKeyWithValue("node-label", to.Ptr("test-value")))
		Expect(updatedCluster.Properties.APIServerAccessProfile).ToNot(BeNil())
		authorizedIPranges := updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges
		Expect(authorizedIPranges).To(HaveLen(1))
		Expect(*authorizedIPranges[0]).To(Equal("test-ip-range"))
		Expect(updatedCluster.Properties.LinuxProfile).ToNot(BeNil())
		Expect(updatedCluster.Properties.LinuxProfile.AdminUsername).To(Equal(clusterSpec.LinuxAdminUsername))
		sshPublicKeys := updatedCluster.Properties.LinuxProfile.SSH.PublicKeys
		Expect(sshPublicKeys).To(HaveLen(1))
		Expect(sshPublicKeys[0].KeyData).To(Equal(clusterSpec.LinuxSSHPublicKey))
		Expect(updatedCluster.Properties.NetworkProfile).ToNot(BeNil())
		Expect(*updatedCluster.Properties.NetworkProfile.NetworkPlugin).To(Equal(armcontainerservice.NetworkPluginAzure))
		Expect(*updatedCluster.Properties.NetworkProfile.NetworkPolicy).To(Equal(armcontainerservice.NetworkPolicyAzure))
		Expect(updatedCluster.Properties.NetworkProfile.DNSServiceIP).To(Equal(clusterSpec.NetworkDNSServiceIP))
		Expect(updatedCluster.Properties.NetworkProfile.PodCidr).To(Equal(clusterSpec.NetworkPodCIDR))
		Expect(updatedCluster.Properties.NetworkProfile.ServiceCidr).To(Equal(clusterSpec.NetworkServiceCIDR))
		Expect(*updatedCluster.Properties.NetworkProfile.LoadBalancerSKU).To(Equal(armcontainerservice.LoadBalancerSKUStandard))
		Expect(updatedCluster.Properties.ServicePrincipalProfile).ToNot(BeNil())
		Expect(updatedCluster.Properties.ServicePrincipalProfile.ClientID).To(Equal(to.Ptr(cred.ClientID)))
		Expect(updatedCluster.Properties.ServicePrincipalProfile.Secret).To(Equal(to.Ptr(cred.ClientSecret)))
		Expect(updatedCluster.Tags).To(HaveKeyWithValue("test-tag", to.Ptr("test-value")))
	})

	It("shouldn't update kubernetes version if it's not specified", func() {
		clusterSpec.KubernetesVersion = nil
		desiredCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "phase")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := updateCluster(*desiredCluster, *actualCluster, false)
		Expect(updatedCluster.Properties.KubernetesVersion).To(BeNil())
	})

	It("shouldn't add new agent pool profile if it already exists", func() {
		actualCluster.Properties.AgentPoolProfiles = []*armcontainerservice.ManagedClusterAgentPoolProfile{
			{
				Name: to.Ptr("test-nodepool"),
			},
		}
		desiredCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "phase")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := updateCluster(*desiredCluster, *actualCluster, false)
		agentPoolProfiles := updatedCluster.Properties.AgentPoolProfiles
		Expect(agentPoolProfiles).To(HaveLen(1))
	})

	It("should clear authorized IP ranges for non imported clusters", func() {
		actualCluster.Properties.APIServerAccessProfile = &armcontainerservice.ManagedClusterAPIServerAccessProfile{
			AuthorizedIPRanges: []*string{to.Ptr("test-ip-range"), to.Ptr("test-ip-range-2")},
		}

		clusterSpec.AuthorizedIPRanges = nil
		desiredCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "phase")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := updateCluster(*desiredCluster, *actualCluster, false)
		Expect(updatedCluster.Properties.APIServerAccessProfile).ToNot(BeNil())
		Expect(updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges).ToNot(BeNil())
		authorizedIPranges := updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges
		Expect(authorizedIPranges).To(HaveLen(0))

		clusterSpec.AuthorizedIPRanges = to.Ptr([]string{})
		desiredCluster, err = createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "phase")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster = updateCluster(*desiredCluster, *actualCluster, false)
		Expect(updatedCluster.Properties.APIServerAccessProfile).ToNot(BeNil())
		Expect(updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges).ToNot(BeNil())
		authorizedIPranges = updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges
		Expect(authorizedIPranges).To(HaveLen(0))
	})

	It("should overwrite authorized IP range for non imported clusters", func() {
		clusterSpec.AuthorizedIPRanges = to.Ptr([]string{"test-ip-range-new1", "test-ip-range-new2"})

		actualCluster.Properties.APIServerAccessProfile = &armcontainerservice.ManagedClusterAPIServerAccessProfile{
			AuthorizedIPRanges: []*string{to.Ptr("test-ip-range"), to.Ptr("test-ip-range-2")},
		}

		desiredCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "phase")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := updateCluster(*desiredCluster, *actualCluster, false)
		Expect(updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges).To(Equal(actualCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges))
		Expect(updatedCluster.Properties.APIServerAccessProfile).ToNot(BeNil())
		authorizedIPranges := updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges
		Expect(authorizedIPranges).To(HaveLen(2))
		Expect(*authorizedIPranges[0]).To(Equal("test-ip-range-new1"))
		Expect(*authorizedIPranges[1]).To(Equal("test-ip-range-new2"))

		clusterSpec.AuthorizedIPRanges = to.Ptr([]string{"test-ip-range-new1", "test-ip-range-new2"})
	})

	It("should merge authorized IP range if it already exists for imported cluster", func() {
		clusterSpec.AuthorizedIPRanges = to.Ptr([]string{"test-ip-range-new1", "test-ip-range-new2", "test-ip-range"})

		actualCluster.Properties.APIServerAccessProfile = &armcontainerservice.ManagedClusterAPIServerAccessProfile{
			AuthorizedIPRanges: []*string{to.Ptr("test-ip-range"), to.Ptr("test-ip-range-2")},
		}

		desiredCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "phase")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := updateCluster(*desiredCluster, *actualCluster, true)
		Expect(updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges).To(Equal(actualCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges))
		Expect(updatedCluster.Properties.APIServerAccessProfile).ToNot(BeNil())
		authorizedIPranges := updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges
		Expect(authorizedIPranges).To(HaveLen(4))
		Expect(*authorizedIPranges[0]).To(Equal("test-ip-range"))
		Expect(*authorizedIPranges[1]).To(Equal("test-ip-range-2"))
		Expect(*authorizedIPranges[2]).To(Equal("test-ip-range-new1"))
		Expect(*authorizedIPranges[3]).To(Equal("test-ip-range-new2"))
	})

	It("shoudn't change authorized IP range if it is empty or nil to imported cluster ", func() {
		actualCluster.Properties.APIServerAccessProfile = &armcontainerservice.ManagedClusterAPIServerAccessProfile{
			AuthorizedIPRanges: []*string{to.Ptr("test-ip-range"), to.Ptr("test-ip-range-2")},
		}
		clusterSpec.AuthorizedIPRanges = nil
		desiredCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "phase")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := updateCluster(*desiredCluster, *actualCluster, true)
		Expect(updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges).To(Equal(actualCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges))
		Expect(updatedCluster.Properties.APIServerAccessProfile).ToNot(BeNil())
		authorizedIPranges := updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges
		Expect(authorizedIPranges).To(HaveLen(2))
		Expect(*authorizedIPranges[0]).To(Equal("test-ip-range"))
		Expect(*authorizedIPranges[1]).To(Equal("test-ip-range-2"))

		clusterSpec.AuthorizedIPRanges = to.Ptr([]string{})
		desiredCluster, err = createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "phase")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster = updateCluster(*desiredCluster, *actualCluster, true)
		Expect(updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges).To(Equal(actualCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges))
		Expect(updatedCluster.Properties.APIServerAccessProfile).ToNot(BeNil())
		authorizedIPranges = updatedCluster.Properties.APIServerAccessProfile.AuthorizedIPRanges
		Expect(authorizedIPranges).To(HaveLen(2))
		Expect(*authorizedIPranges[0]).To(Equal("test-ip-range"))
		Expect(*authorizedIPranges[1]).To(Equal("test-ip-range-2"))
	})

	It("shouldn't update linux profile if it's not specified", func() {
		clusterSpec.LinuxAdminUsername = nil
		clusterSpec.LinuxSSHPublicKey = nil
		desiredCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "phase")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := updateCluster(*desiredCluster, *actualCluster, false)
		Expect(updatedCluster.Properties.LinuxProfile).To(BeNil())
	})

	It("shouldn't update service principal if phase is active or updating", func() {
		desiredCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "active")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := updateCluster(*desiredCluster, *actualCluster, false)
		Expect(updatedCluster.Properties.ServicePrincipalProfile).To(BeNil())
	})

	It("shouldn't update tags if not specified in cluster spec", func() {
		clusterSpec.Tags = nil
		desiredCluster, err := createManagedCluster(ctx, cred, workplacesClientMock, clusterSpec, "phase")
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := updateCluster(*desiredCluster, *actualCluster, false)
		Expect(updatedCluster.Tags).To(HaveLen(0))
	})
})

var _ = Describe("UpdateCluster", func() {
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
		clusterSpec = &aksv1.AKSClusterConfigSpec{
			ResourceGroup: "test-rg",
			ClusterName:   "test-cluster",
		}
	})

	AfterEach(func() {
		mockController.Finish()
	})

	It("should successfully update cluster", func() {
		clusterClientMock.EXPECT().Get(ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, nil).Return(armcontainerservice.ManagedClustersClientGetResponse{}, nil)
		clusterClientMock.EXPECT().BeginCreateOrUpdate(ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, gomock.Any(), nil).Return(pollerMock, nil)
		pollerMock.EXPECT().PollUntilDone(ctx, nil).Return(armcontainerservice.ManagedClustersClientCreateOrUpdateResponse{}, nil)
		Expect(UpdateCluster(ctx, &Credentials{}, clusterClientMock, workplacesClientMock, clusterSpec, "active")).To(Succeed())
	})

	It("should fail when createManagedCluster returns error", func() {
		clusterSpec.Monitoring = to.Ptr(true)
		Expect(UpdateCluster(ctx, &Credentials{}, clusterClientMock, workplacesClientMock, clusterSpec, "active")).ToNot(Succeed())
	})

	It("should fail when azure API returns error on Get() request", func() {
		clusterClientMock.EXPECT().Get(ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, nil).Return(armcontainerservice.ManagedClustersClientGetResponse{}, errors.New("test error"))
		Expect(UpdateCluster(ctx, &Credentials{}, clusterClientMock, workplacesClientMock, clusterSpec, "active")).ToNot(Succeed())
	})

	It("should fail when azure API returns error on CreateOrUpdate() request", func() {
		clusterClientMock.EXPECT().Get(ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, nil).Return(armcontainerservice.ManagedClustersClientGetResponse{}, nil)
		clusterClientMock.EXPECT().BeginCreateOrUpdate(ctx, clusterSpec.ResourceGroup, clusterSpec.ClusterName, gomock.Any(), nil).Return(pollerMock, errors.New("test error"))
		Expect(UpdateCluster(ctx, &Credentials{}, clusterClientMock, workplacesClientMock, clusterSpec, "active")).ToNot(Succeed())
	})
})

var _ = Describe("validateUpdate", func() {
	var (
		desiredCluster *armcontainerservice.ManagedCluster
		actualCluster  *armcontainerservice.ManagedCluster
	)

	BeforeEach(func() {
		desiredCluster = &armcontainerservice.ManagedCluster{
			Name:     to.Ptr("test-cluster"),
			Location: to.Ptr("test-location"),
			Properties: &armcontainerservice.ManagedClusterProperties{
				DNSPrefix: to.Ptr("test-dns-prefix"),
				APIServerAccessProfile: &armcontainerservice.ManagedClusterAPIServerAccessProfile{
					EnablePrivateCluster: to.Ptr(true),
				},
			},
		}
		actualCluster = &armcontainerservice.ManagedCluster{
			Name:     to.Ptr("test-cluster"),
			Location: to.Ptr("test-location"),
			Properties: &armcontainerservice.ManagedClusterProperties{
				DNSPrefix: to.Ptr("test-dns-prefix"),
				APIServerAccessProfile: &armcontainerservice.ManagedClusterAPIServerAccessProfile{
					EnablePrivateCluster: to.Ptr(true),
				},
			},
		}
	})

	It("should be true if cluster can be updated", func() {
		Expect(validateUpdate(*desiredCluster, *actualCluster)).To(BeTrue())
	})

	It("should be false if cluster name is different", func() {
		desiredCluster.Name = to.Ptr("test-cluster-2")
		Expect(validateUpdate(*desiredCluster, *actualCluster)).To(BeFalse())
	})

	It("should be false if cluster location is different", func() {
		desiredCluster.Location = to.Ptr("test-location-2")
		Expect(validateUpdate(*desiredCluster, *actualCluster)).To(BeFalse())
	})

	It("should be false if cluster dns prefix is different", func() {
		desiredCluster.Properties.DNSPrefix = to.Ptr("test-dns-prefix-2")
		Expect(validateUpdate(*desiredCluster, *actualCluster)).To(BeFalse())
	})

	It("should be false if cluster private cluster is different", func() {
		desiredCluster.Properties.APIServerAccessProfile.EnablePrivateCluster = to.Ptr(false)
		Expect(validateUpdate(*desiredCluster, *actualCluster)).To(BeFalse())
	})
})
