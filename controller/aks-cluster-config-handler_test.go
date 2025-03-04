package controller

import (
	"bytes"
	"errors"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher/aks-operator/pkg/aks"
	"github.com/rancher/aks-operator/pkg/aks/services/mock_services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	aksv1controllers "github.com/rancher/aks-operator/pkg/generated/controllers/aks.cattle.io"
	"github.com/rancher/aks-operator/pkg/test"
	"github.com/rancher/aks-operator/pkg/utils"
	"github.com/rancher/wrangler/v3/pkg/generated/controllers/core"
	"github.com/sirupsen/logrus"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const kubeconfigYAML = `
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCnRlc3QKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
    server: https://test.com
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
kind: Config
preferences: {}
users:
- name: test
  user:
    client-certificate-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCnRlc3QKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
    client-key-data: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQp0ZXN0Ci0tLS0tRU5EIFJTQSBQUklWQVRFIEtFWS0tLS0tCg==
    token: dGVzdA==`

var _ = Describe("importCluster", func() {
	var (
		handler           *Handler
		mockController    *gomock.Controller
		clusterClientMock *mock_services.MockManagedClustersClientInterface
		aksConfig         *aksv1.AKSClusterConfig
		caSecret          *corev1.Secret
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		clusterClientMock = mock_services.NewMockManagedClustersClientInterface(mockController)

		aksConfig = &aksv1.AKSClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: aksv1.AKSClusterConfigSpec{
				ResourceGroup: "test",
				ClusterName:   "test",
			},
		}

		Expect(cl.Create(ctx, aksConfig)).To(Succeed())

		caSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      aksConfig.Name,
				Namespace: aksConfig.Namespace,
			},
		}

		handler = &Handler{
			aksCC:        aksFactory.Aks().V1().AKSClusterConfig(),
			secrets:      coreFactory.Core().V1().Secret(),
			secretsCache: coreFactory.Core().V1().Secret().Cache(),
			azureClients: azureClients{
				clustersClient: clusterClientMock,
			},
		}
	})

	AfterEach(func() {
		Expect(test.CleanupAndWait(ctx, cl, caSecret, aksConfig)).To(Succeed())
	})

	It("should create CA secret and update status", func() {
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfig.Spec.ResourceGroup, aksConfig.Spec.ClusterName, "clusterAdmin", nil).
			Return(armcontainerservice.ManagedClustersClientGetAccessProfileResponse{
				ManagedClusterAccessProfile: armcontainerservice.ManagedClusterAccessProfile{
					Properties: &armcontainerservice.AccessProfile{
						KubeConfig: []byte(kubeconfigYAML),
					},
				},
			}, nil)

		gotAKSConfig, err := handler.importCluster(aksConfig)
		Expect(err).ToNot(HaveOccurred())
		Expect(gotAKSConfig.Status.Phase).To(Equal(aksConfigActivePhase))

		Expect(cl.Get(ctx, client.ObjectKeyFromObject(caSecret), caSecret)).To(Succeed())
		Expect(caSecret.OwnerReferences).To(HaveLen(1))
		Expect(caSecret.OwnerReferences[0].Name).To(Equal(gotAKSConfig.Name))
		Expect(caSecret.OwnerReferences[0].Kind).To(Equal(aksClusterConfigKind))
		Expect(caSecret.OwnerReferences[0].APIVersion).To(Equal(aksv1.SchemeGroupVersion.String()))
		Expect(caSecret.Data["endpoint"]).To(Equal([]byte("https://test.com")))
		Expect(caSecret.Data["ca"]).To(Equal([]byte("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCnRlc3QKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=")))
	})

	It("don't return error if secret already exists", func() {
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfig.Spec.ResourceGroup, aksConfig.Spec.ClusterName, "clusterAdmin", nil).
			Return(
				armcontainerservice.ManagedClustersClientGetAccessProfileResponse{
					ManagedClusterAccessProfile: armcontainerservice.ManagedClusterAccessProfile{
						Properties: &armcontainerservice.AccessProfile{
							KubeConfig: []byte(kubeconfigYAML),
						},
					},
				}, nil).AnyTimes()

		gotAKSConfig, err := handler.importCluster(aksConfig)
		Expect(err).ToNot(HaveOccurred())
		Expect(gotAKSConfig.Status.Phase).To(Equal(aksConfigActivePhase))

		Expect(cl.Get(ctx, client.ObjectKeyFromObject(aksConfig), aksConfig)).To(Succeed()) // refresh aksConfig, an update call will be made later
		gotAKSConfig, err = handler.importCluster(aksConfig)                                // call importCluster again to test with created secret
		Expect(err).ToNot(HaveOccurred())
		Expect(gotAKSConfig.Status.Phase).To(Equal(aksConfigActivePhase))
	})

	It("should return error if something fails", func() {
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfig.Spec.ResourceGroup, aksConfig.Spec.ClusterName, "clusterAdmin", nil).
			Return(armcontainerservice.ManagedClustersClientGetAccessProfileResponse{
				ManagedClusterAccessProfile: armcontainerservice.ManagedClusterAccessProfile{},
			}, errors.New("error"))

		gotAKSConfig, err := handler.importCluster(aksConfig)
		Expect(err).To(HaveOccurred())
		Expect(gotAKSConfig).NotTo(BeNil())
	})

})

var _ = Describe("getClusterKubeConfig", func() {
	var (
		handler           *Handler
		mockController    *gomock.Controller
		clusterClientMock *mock_services.MockManagedClustersClientInterface
		aksConfigSpec     *aksv1.AKSClusterConfigSpec
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		clusterClientMock = mock_services.NewMockManagedClustersClientInterface(mockController)
		aksConfigSpec = &aksv1.AKSClusterConfigSpec{
			ResourceGroup: "test",
			ClusterName:   "test",
		}
		handler = &Handler{
			azureClients: azureClients{
				clustersClient: clusterClientMock,
			},
		}
	})

	It("should successfully return kubeconfig", func() {
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfigSpec.ResourceGroup, aksConfigSpec.ClusterName, "clusterAdmin", nil).
			Return(armcontainerservice.ManagedClustersClientGetAccessProfileResponse{
				ManagedClusterAccessProfile: armcontainerservice.ManagedClusterAccessProfile{
					Properties: &armcontainerservice.AccessProfile{
						KubeConfig: []byte(kubeconfigYAML),
					},
				},
			}, nil)

		config, err := handler.getClusterKubeConfig(ctx, aksConfigSpec)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).ToNot(BeNil())
	})

	It("should return error if azure request fails", func() {
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfigSpec.ResourceGroup, aksConfigSpec.ClusterName, "clusterAdmin", nil).
			Return(armcontainerservice.ManagedClustersClientGetAccessProfileResponse{
				ManagedClusterAccessProfile: armcontainerservice.ManagedClusterAccessProfile{},
			}, errors.New("error"))

		_, err := handler.getClusterKubeConfig(ctx, aksConfigSpec)
		Expect(err).To(HaveOccurred())
	})

	It("should return error if config is failed to be created", func() {
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfigSpec.ResourceGroup, aksConfigSpec.ClusterName, "clusterAdmin", nil).
			Return(armcontainerservice.ManagedClustersClientGetAccessProfileResponse{
				ManagedClusterAccessProfile: armcontainerservice.ManagedClusterAccessProfile{
					Properties: &armcontainerservice.AccessProfile{
						KubeConfig: []byte("invalid"),
					},
				},
			}, nil)

		_, err := handler.getClusterKubeConfig(ctx, aksConfigSpec)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("validateConfig", func() {
	var (
		handler           *Handler
		aksConfig         *aksv1.AKSClusterConfig
		credentialsSecret *corev1.Secret
	)

	BeforeEach(func() {
		credentialsSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "azure-credential-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"azurecredentialConfig-tenantId":       []byte("test"),
				"azurecredentialConfig-subscriptionId": []byte("test"),
				"azurecredentialConfig-clientId":       []byte("test"),
				"azurecredentialConfig-clientSecret":   []byte("test"),
			},
		}

		Expect(cl.Create(ctx, credentialsSecret)).To(Succeed())

		aksConfig = &aksv1.AKSClusterConfig{
			Spec: aksv1.AKSClusterConfigSpec{
				AzureCredentialSecret: credentialsSecret.Namespace + ":" + credentialsSecret.Name,
				AuthBaseURL:           to.Ptr("test"),
				BaseURL:               to.Ptr("test"),
				ResourceLocation:      "test",
				ResourceGroup:         "test",
				ClusterName:           "test",
				KubernetesVersion:     to.Ptr("test"),
				DNSPrefix:             to.Ptr("test"),
				NodePools: []aksv1.AKSNodePool{
					{
						Name:         to.Ptr("test1"),
						Count:        to.Ptr(int32(1)),
						MaxPods:      to.Ptr(int32(1)),
						VMSize:       "test",
						OsDiskSizeGB: to.Ptr(int32(1)),
						OsDiskType:   "test",
						Mode:         "System",
						OsType:       "test",
					},
					{
						Name:         to.Ptr("test2"),
						Count:        to.Ptr(int32(1)),
						MaxPods:      to.Ptr(int32(1)),
						VMSize:       "test",
						OsDiskSizeGB: to.Ptr(int32(1)),
						OsDiskType:   "test",
						Mode:         "User",
						OsType:       "test",
					},
				},
				NetworkPlugin:           to.Ptr(string(armcontainerservice.NetworkPluginAzure)),
				NetworkPolicy:           to.Ptr(string(armcontainerservice.NetworkPolicyAzure)),
				VirtualNetwork:          to.Ptr("test"),
				Subnet:                  to.Ptr("test"),
				NetworkDNSServiceIP:     to.Ptr("test"),
				NetworkDockerBridgeCIDR: to.Ptr("test"),
				NetworkServiceCIDR:      to.Ptr("test"),
			},
		}

		handler = &Handler{
			aksCC:        aksFactory.Aks().V1().AKSClusterConfig(),
			secrets:      coreFactory.Core().V1().Secret(),
			secretsCache: coreFactory.Core().V1().Secret().Cache(),
		}
	})

	AfterEach(func() {
		test.CleanupAndWait(ctx, cl, credentialsSecret)
	})

	It("should successfully validate aks config", func() {
		Expect(handler.validateConfig(aksConfig)).To(Succeed())
	})

	It("should successfully validate aks config when cluster is imported", func() {
		aksConfig.Spec.Imported = true
		aksConfig.Spec.KubernetesVersion = nil
		aksConfig.Spec.DNSPrefix = nil
		aksConfig.Spec.NodePools = nil

		Expect(handler.validateConfig(aksConfig)).To(Succeed())
	})

	It("should successfully validate if user defined routing is used with subnet", func() {
		aksConfig.Spec.OutboundType = to.Ptr("userDefinedRouting")
		aksConfig.Spec.Subnet = to.Ptr("test-subnet-1")
		aksConfig.Spec.NetworkPlugin = to.Ptr(string(armcontainerservice.NetworkPluginAzure))
		Expect(handler.validateConfig(aksConfig)).To(Succeed())
	})

	It("should fail if listing aks cluster config fails", func() {
		brokenAksFactory, err := aksv1controllers.NewFactoryFromConfig(&rest.Config{})
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenAksFactory).NotTo(BeNil())

		handler.aksCC = brokenAksFactory.Aks().V1().AKSClusterConfig()
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fails validate aks config if getting secret fails", func() {
		brokenSecretFactory, err := core.NewFactoryFromConfig(&rest.Config{})
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenSecretFactory).NotTo(BeNil())

		handler.secrets = brokenSecretFactory.Core().V1().Secret()
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if cluster with a similar name exists", func() {
		secondAKSConfig := &aksv1.AKSClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: aksv1.AKSClusterConfigSpec{
				ClusterName: "test",
			},
		}
		Expect(cl.Create(ctx, secondAKSConfig)).To(Succeed())

		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())

		Expect(cl.Delete(ctx, secondAKSConfig)).To(Succeed())
	})

	It("should fail to validate aks config if resource location is empty", func() {
		aksConfig.Spec.ResourceLocation = ""
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if resource group is empty", func() {
		aksConfig.Spec.ResourceGroup = ""
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if cluster name is empty", func() {
		aksConfig.Spec.ClusterName = ""
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if credentials secret name is empty", func() {
		aksConfig.Spec.AzureCredentialSecret = ""
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if kubernetes version is empty", func() {
		aksConfig.Spec.KubernetesVersion = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if dns prefix is empty", func() {
		aksConfig.Spec.DNSPrefix = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pools are empty", func() {
		aksConfig.Spec.NodePools = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pool name is empty", func() {
		aksConfig.Spec.NodePools[0].Name = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pool name is duplicated", func() {
		aksConfig.Spec.NodePools[0].Name = to.Ptr("test")
		aksConfig.Spec.NodePools[1].Name = to.Ptr("test")
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pool count is empty", func() {
		aksConfig.Spec.NodePools[0].Count = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pool max pods is empty", func() {
		aksConfig.Spec.NodePools[0].MaxPods = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pool vm size is empty", func() {
		aksConfig.Spec.NodePools[0].VMSize = ""
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pool os disk size is empty", func() {
		aksConfig.Spec.NodePools[0].OsDiskSizeGB = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pool os disk type is empty", func() {
		aksConfig.Spec.NodePools[0].OsDiskType = ""
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pool mode is empty", func() {
		aksConfig.Spec.NodePools[0].Mode = ""
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pools don't have one node pool with mode system", func() {
		aksConfig.Spec.NodePools[0].Mode = "NotSystem"
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pool os type is empty", func() {
		aksConfig.Spec.NodePools[0].OsType = ""
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail to validate aks config if node pool os type is windows", func() {
		aksConfig.Spec.NodePools[0].OsType = "Windows"
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if network plugin is not azure or kubenet", func() {
		aksConfig.Spec.NetworkPlugin = to.Ptr("invalid")
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if network policy is not azure or calico", func() {
		aksConfig.Spec.NetworkPolicy = to.Ptr("invalid")
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if network policy is azure and network plugin is kubenet", func() {
		aksConfig.Spec.NetworkPlugin = to.Ptr(string(armcontainerservice.NetworkPluginKubenet))
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if virtual network is empty", func() {
		aksConfig.Spec.VirtualNetwork = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if subnet is empty", func() {
		aksConfig.Spec.Subnet = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if network dns service ip is empty", func() {
		aksConfig.Spec.NetworkDNSServiceIP = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if network docker bridge cidr is empty", func() {
		aksConfig.Spec.NetworkDockerBridgeCIDR = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if network pod cidr is empty", func() {
		aksConfig.Spec.NetworkServiceCIDR = nil
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if user defined routing is used and subnet is empty", func() {
		aksConfig.Spec.OutboundType = to.Ptr("userDefinedRouting")
		aksConfig.Spec.Subnet = to.Ptr("")
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if user defined routing is used and kubenet as network plugin", func() {
		aksConfig.Spec.OutboundType = to.Ptr("userDefinedRouting")
		aksConfig.Spec.Subnet = to.Ptr("test-subnet-1")
		aksConfig.Spec.NetworkPlugin = to.Ptr(string(armcontainerservice.NetworkPluginKubenet))
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})
})

var _ = Describe("createCluster", func() {
	var (
		handler                 *Handler
		mockController          *gomock.Controller
		clusterClientMock       *mock_services.MockManagedClustersClientInterface
		resourceGroupClientMock *mock_services.MockResourceGroupsClientInterface
		workplacesClientMock    *mock_services.MockWorkplacesClientInterface
		aksConfig               *aksv1.AKSClusterConfig
		credentialsSecret       *corev1.Secret
		pollerMock              *mock_services.MockPoller[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse]
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		clusterClientMock = mock_services.NewMockManagedClustersClientInterface(mockController)
		resourceGroupClientMock = mock_services.NewMockResourceGroupsClientInterface(mockController)
		workplacesClientMock = mock_services.NewMockWorkplacesClientInterface(mockController)
		pollerMock = mock_services.NewMockPoller[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse](mockController)

		credentialsSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "azure-credential-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"azurecredentialConfig-tenantId":       []byte("test"),
				"azurecredentialConfig-subscriptionId": []byte("test"),
				"azurecredentialConfig-clientId":       []byte("test"),
				"azurecredentialConfig-clientSecret":   []byte("test"),
			},
		}

		Expect(cl.Create(ctx, credentialsSecret)).To(Succeed())

		aksConfig = &aksv1.AKSClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: aksv1.AKSClusterConfigSpec{
				AzureCredentialSecret: credentialsSecret.Namespace + ":" + credentialsSecret.Name,
				AuthBaseURL:           to.Ptr("test"),
				BaseURL:               to.Ptr("test"),
				ResourceLocation:      "test",
				ResourceGroup:         "test",
				ClusterName:           "test",
				KubernetesVersion:     to.Ptr("test"),
				DNSPrefix:             to.Ptr("test"),
				NodePools: []aksv1.AKSNodePool{
					{
						Name:         to.Ptr("test"),
						Count:        to.Ptr(int32(1)),
						MaxPods:      to.Ptr(int32(1)),
						VMSize:       "test",
						OsDiskSizeGB: to.Ptr(int32(1)),
						OsDiskType:   "test",
						Mode:         "System",
						OsType:       "test",
					},
				},
				NetworkPlugin:           to.Ptr(string(armcontainerservice.NetworkPluginAzure)),
				NetworkPolicy:           to.Ptr(string(armcontainerservice.NetworkPolicyAzure)),
				VirtualNetwork:          to.Ptr("test"),
				Subnet:                  to.Ptr("test"),
				NetworkDNSServiceIP:     to.Ptr("test"),
				NetworkDockerBridgeCIDR: to.Ptr("test"),
				NetworkServiceCIDR:      to.Ptr("test"),
			},
		}

		Expect(cl.Create(ctx, aksConfig)).To(Succeed())

		handler = &Handler{
			aksCC:        aksFactory.Aks().V1().AKSClusterConfig(),
			secrets:      coreFactory.Core().V1().Secret(),
			secretsCache: coreFactory.Core().V1().Secret().Cache(),
			azureClients: azureClients{
				clustersClient:       clusterClientMock,
				resourceGroupsClient: resourceGroupClientMock,
				workplacesClient:     workplacesClientMock,
			},
		}
	})

	AfterEach(func() {
		test.CleanupAndWait(ctx, cl, credentialsSecret, aksConfig)
	})

	It("should successfully create aks cluster", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(armcontainerservice.ManagedClustersClientGetResponse{}, errors.New("cluster does not exist"))
		clusterClientMock.EXPECT().BeginCreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(pollerMock, nil)

		resourceGroupClientMock.EXPECT().CheckExistence(gomock.Any(), gomock.Any(), nil).Return(armresources.ResourceGroupsClientCheckExistenceResponse{
			Success: false,
		}, nil)
		resourceGroupClientMock.EXPECT().CreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(armresources.ResourceGroupsClientCreateOrUpdateResponse{}, nil)

		gotAKSConfig, err := handler.createCluster(aksConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(gotAKSConfig.Status.Phase).To(Equal(aksConfigCreatingPhase))
	})

	It("should successfully process imported aks cluster", func() {
		aksConfig.Spec.Imported = true
		gotAKSConfig, err := handler.createCluster(aksConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(gotAKSConfig.Status.Phase).To(Equal(aksConfigImportingPhase))
	})

	It("should fail if validate config fails", func() {
		aksConfig.Spec.ClusterName = ""
		_, err := handler.createCluster(aksConfig)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if cluster already exists", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(armcontainerservice.ManagedClustersClientGetResponse{
			ManagedCluster: armcontainerservice.ManagedCluster{
				Name: to.Ptr(aksConfig.Name),
			},
		}, nil)

		_, err := handler.createCluster(aksConfig)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if resource group failed to be created", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(armcontainerservice.ManagedClustersClientGetResponse{}, errors.New("cluster does not exist"))

		resourceGroupClientMock.EXPECT().CheckExistence(gomock.Any(), gomock.Any(), nil).Return(armresources.ResourceGroupsClientCheckExistenceResponse{
			Success: false,
		}, nil)

		resourceGroupClientMock.EXPECT().CreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(armresources.ResourceGroupsClientCreateOrUpdateResponse{}, errors.New("error"))

		_, err := handler.createCluster(aksConfig)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if cluster failed to be created", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(armcontainerservice.ManagedClustersClientGetResponse{}, errors.New("cluster does not exist"))

		clusterClientMock.EXPECT().BeginCreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(pollerMock, errors.New("error"))

		resourceGroupClientMock.EXPECT().CheckExistence(gomock.Any(), gomock.Any(), nil).Return(armresources.ResourceGroupsClientCheckExistenceResponse{
			Success: false,
		}, nil)

		resourceGroupClientMock.EXPECT().CreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(armresources.ResourceGroupsClientCreateOrUpdateResponse{}, nil)

		_, err := handler.createCluster(aksConfig)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("waitForCluster", func() {
	var (
		handler           *Handler
		mockController    *gomock.Controller
		clusterClientMock *mock_services.MockManagedClustersClientInterface
		aksConfig         *aksv1.AKSClusterConfig
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		clusterClientMock = mock_services.NewMockManagedClustersClientInterface(mockController)

		aksConfig = &aksv1.AKSClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: aksv1.AKSClusterConfigSpec{
				ResourceGroup: "test",
				ClusterName:   "test",
			},
		}

		Expect(cl.Create(ctx, aksConfig)).To(Succeed())

		handler = &Handler{
			aksCC:        aksFactory.Aks().V1().AKSClusterConfig(),
			secrets:      coreFactory.Core().V1().Secret(),
			secretsCache: coreFactory.Core().V1().Secret().Cache(),
			azureClients: azureClients{
				clustersClient: clusterClientMock,
			},
			aksEnqueueAfter: aksFactory.Aks().V1().AKSClusterConfig().EnqueueAfter,
		}
	})

	AfterEach(func() {
		Expect(test.CleanupAndWait(ctx, cl, aksConfig)).To(Succeed())
	})

	It("should successfully wait for cluster", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(
			armcontainerservice.ManagedClustersClientGetResponse{
				ManagedCluster: armcontainerservice.ManagedCluster{
					Properties: &armcontainerservice.ManagedClusterProperties{
						ProvisioningState: to.Ptr(ClusterStatusSucceeded),
					},
				},
			},
			nil)

		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfig.Spec.ResourceGroup, aksConfig.Spec.ClusterName, "clusterAdmin", nil).
			Return(armcontainerservice.ManagedClustersClientGetAccessProfileResponse{
				ManagedClusterAccessProfile: armcontainerservice.ManagedClusterAccessProfile{
					Properties: &armcontainerservice.AccessProfile{
						KubeConfig: []byte(kubeconfigYAML),
					},
				},
			}, nil)

		gotAKSConfig, err := handler.waitForCluster(aksConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(gotAKSConfig.Status.Phase).To(Equal(aksConfigActivePhase))
	})

	It("should continue waiting for cluster if it's still creating", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(
			armcontainerservice.ManagedClustersClientGetResponse{
				ManagedCluster: armcontainerservice.ManagedCluster{
					Properties: &armcontainerservice.ManagedClusterProperties{
						ProvisioningState: to.Ptr("Creating"),
					},
				},
			},
			nil)

		_, err := handler.waitForCluster(aksConfig)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should fail if azure client failed to get cluster", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(
			armcontainerservice.ManagedClustersClientGetResponse{
				ManagedCluster: armcontainerservice.ManagedCluster{},
			}, errors.New("error"))

		_, err := handler.waitForCluster(aksConfig)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if cluster creation failed", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(
			armcontainerservice.ManagedClustersClientGetResponse{
				ManagedCluster: armcontainerservice.ManagedCluster{
					Properties: &armcontainerservice.ManagedClusterProperties{
						ProvisioningState: to.Ptr(ClusterStatusFailed),
					},
				},
			},
			nil)

		_, err := handler.waitForCluster(aksConfig)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if CA secret creation failed", func() {
		brokenSecretFactory, err := core.NewFactoryFromConfig(&rest.Config{})
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenSecretFactory).NotTo(BeNil())

		handler.secrets = brokenSecretFactory.Core().V1().Secret()
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())

		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(
			armcontainerservice.ManagedClustersClientGetResponse{
				ManagedCluster: armcontainerservice.ManagedCluster{
					Properties: &armcontainerservice.ManagedClusterProperties{
						ProvisioningState: to.Ptr(ClusterStatusSucceeded),
					},
				},
			},
			nil)

		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfig.Spec.ResourceGroup, aksConfig.Spec.ClusterName, "clusterAdmin", nil).
			Return(
				armcontainerservice.ManagedClustersClientGetAccessProfileResponse{
					ManagedClusterAccessProfile: armcontainerservice.ManagedClusterAccessProfile{
						Properties: &armcontainerservice.AccessProfile{
							KubeConfig: []byte(kubeconfigYAML),
						},
					},
				},
				nil)

		_, err = handler.waitForCluster(aksConfig)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("buildUpstreamClusterState", func() {
	var (
		handler           *Handler
		mockController    *gomock.Controller
		clusterClientMock *mock_services.MockManagedClustersClientInterface
		credentials       *aks.Credentials
		aksConfig         *aksv1.AKSClusterConfig
		clusterState      *armcontainerservice.ManagedCluster
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		clusterClientMock = mock_services.NewMockManagedClustersClientInterface(mockController)

		credentials = &aks.Credentials{
			AuthBaseURL: to.Ptr("test"),
			BaseURL:     to.Ptr("test"),
		}

		aksConfig = &aksv1.AKSClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: aksv1.AKSClusterConfigSpec{},
		}

		clusterState = &armcontainerservice.ManagedCluster{
			Properties: &armcontainerservice.ManagedClusterProperties{
				KubernetesVersion: to.Ptr("test"),
				DNSPrefix:         to.Ptr("test"),
				AgentPoolProfiles: []*armcontainerservice.ManagedClusterAgentPoolProfile{
					{
						Name:                to.Ptr("test"),
						Count:               to.Ptr(int32(1)),
						MaxPods:             to.Ptr(int32(1)),
						VMSize:              to.Ptr("Standard_DS2_v2"),
						OSDiskSizeGB:        to.Ptr(int32(1)),
						OSType:              to.Ptr(armcontainerservice.OSTypeLinux),
						Mode:                to.Ptr(armcontainerservice.AgentPoolModeUser),
						OrchestratorVersion: to.Ptr("test"),
						AvailabilityZones:   utils.ConvertToSliceOfPointers(to.Ptr([]string{"test"})),
						EnableAutoScaling:   to.Ptr(true),
						MaxCount:            to.Ptr(int32(1)),
						MinCount:            to.Ptr(int32(1)),
						VnetSubnetID:        to.Ptr("test"),
						NodeLabels:          aks.StringMapPtr(map[string]string{"test": "test"}),
						NodeTaints:          utils.ConvertToSliceOfPointers(to.Ptr([]string{"test"})),
						UpgradeSettings: &armcontainerservice.AgentPoolUpgradeSettings{
							MaxSurge: to.Ptr("test"),
						},
					},
				},
				NetworkProfile: &armcontainerservice.NetworkProfile{
					NetworkPlugin:   to.Ptr(armcontainerservice.NetworkPluginAzure),
					DNSServiceIP:    to.Ptr("test"),
					ServiceCidr:     to.Ptr("test"),
					NetworkPolicy:   to.Ptr(armcontainerservice.NetworkPolicyAzure),
					PodCidr:         to.Ptr("test"),
					LoadBalancerSKU: to.Ptr(armcontainerservice.LoadBalancerSKUStandard),
				},
				LinuxProfile: &armcontainerservice.LinuxProfile{
					AdminUsername: to.Ptr("test"),
					SSH: &armcontainerservice.SSHConfiguration{
						PublicKeys: []*armcontainerservice.SSHPublicKey{
							{
								KeyData: to.Ptr("test"),
							},
						},
					},
				},
				AddonProfiles: map[string]*armcontainerservice.ManagedClusterAddonProfile{
					"httpApplicationRouting": {
						Enabled: to.Ptr(true),
					},
					"omsAgent": {
						Enabled: to.Ptr(true),
						Config: map[string]*string{
							"logAnalyticsWorkspaceResourceID": to.Ptr("/workspaces/test/resourcegroups/test/"),
						},
					},
				},
				APIServerAccessProfile: &armcontainerservice.ManagedClusterAPIServerAccessProfile{
					EnablePrivateCluster: to.Ptr(true),
					AuthorizedIPRanges:   utils.ConvertToSliceOfPointers(to.Ptr([]string{"test"})),
					PrivateDNSZone:       to.Ptr("test-private-dns-zone-id"),
				},
			},
			Tags: aks.StringMapPtr(map[string]string{"test": "test"}),
		}

		handler = &Handler{
			azureClients: azureClients{
				clustersClient: clusterClientMock,
			},
		}
	})

	It("should build upstream cluster state", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(
			armcontainerservice.ManagedClustersClientGetResponse{
				ManagedCluster: *clusterState,
			}, nil)

		upstreamSpec, err := handler.buildUpstreamClusterState(ctx, credentials, &aksConfig.Spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(upstreamSpec).NotTo(BeNil())

		Expect(upstreamSpec.KubernetesVersion).To(Equal(clusterState.Properties.KubernetesVersion))
		Expect(upstreamSpec.DNSPrefix).To(Equal(clusterState.Properties.DNSPrefix))
		Expect(upstreamSpec.Tags).To(Equal(aks.StringMap(clusterState.Tags)))
		Expect(upstreamSpec.NodePools).To(HaveLen(1))
		nodePools := clusterState.Properties.AgentPoolProfiles
		Expect(upstreamSpec.NodePools[0].Name).To(Equal(nodePools[0].Name))
		Expect(upstreamSpec.NodePools[0].Count).To(Equal(nodePools[0].Count))
		Expect(upstreamSpec.NodePools[0].MaxPods).To(Equal(nodePools[0].MaxPods))
		Expect(upstreamSpec.NodePools[0].VMSize).To(Equal(*nodePools[0].VMSize))
		Expect(upstreamSpec.NodePools[0].OsDiskSizeGB).To(Equal(nodePools[0].OSDiskSizeGB))
		Expect(upstreamSpec.NodePools[0].OsType).To(Equal(string(*nodePools[0].OSType)))
		Expect(upstreamSpec.NodePools[0].Mode).To(Equal(string(*nodePools[0].Mode)))
		Expect(upstreamSpec.NodePools[0].OrchestratorVersion).To(Equal(nodePools[0].OrchestratorVersion))
		Expect(upstreamSpec.NodePools[0].AvailabilityZones).To(Equal(utils.ConvertToPointerOfSlice(nodePools[0].AvailabilityZones)))
		Expect(upstreamSpec.NodePools[0].EnableAutoScaling).To(Equal(nodePools[0].EnableAutoScaling))
		Expect(upstreamSpec.NodePools[0].MaxCount).To(Equal(nodePools[0].MaxCount))
		Expect(upstreamSpec.NodePools[0].MinCount).To(Equal(nodePools[0].MinCount))
		Expect(upstreamSpec.NodePools[0].VnetSubnetID).To(Equal(nodePools[0].VnetSubnetID))
		Expect(upstreamSpec.NodePools[0].NodeLabels).To(Equal(nodePools[0].NodeLabels))
		Expect(upstreamSpec.NodePools[0].NodeTaints).To(Equal(utils.ConvertToPointerOfSlice(nodePools[0].NodeTaints)))
		Expect(upstreamSpec.NodePools[0].MaxSurge).To(Equal(nodePools[0].UpgradeSettings.MaxSurge))
		Expect(upstreamSpec.NetworkPlugin).To(Equal(to.Ptr(string(*clusterState.Properties.NetworkProfile.NetworkPlugin))))
		Expect(upstreamSpec.NetworkDNSServiceIP).To(Equal(clusterState.Properties.NetworkProfile.DNSServiceIP))
		Expect(upstreamSpec.NetworkServiceCIDR).To(Equal(clusterState.Properties.NetworkProfile.ServiceCidr))
		Expect(upstreamSpec.NetworkPolicy).To(Equal(to.Ptr(string(*clusterState.Properties.NetworkProfile.NetworkPolicy))))
		Expect(upstreamSpec.NetworkPodCIDR).To(Equal(clusterState.Properties.NetworkProfile.PodCidr))
		Expect(upstreamSpec.LoadBalancerSKU).To(Equal(to.Ptr(string(*clusterState.Properties.NetworkProfile.LoadBalancerSKU))))
		Expect(upstreamSpec.LinuxAdminUsername).To(Equal(clusterState.Properties.LinuxProfile.AdminUsername))
		Expect(upstreamSpec.LinuxSSHPublicKey).To(Equal((clusterState.Properties.LinuxProfile.SSH.PublicKeys)[0].KeyData))
		Expect(upstreamSpec.HTTPApplicationRouting).To(Equal(to.Ptr(*clusterState.Properties.AddonProfiles["httpApplicationRouting"].Enabled)))
		Expect(upstreamSpec.Monitoring).To(Equal(to.Ptr(*clusterState.Properties.AddonProfiles["omsAgent"].Enabled)))
		Expect(upstreamSpec.LogAnalyticsWorkspaceGroup).To(Equal(to.Ptr("test")))
		Expect(upstreamSpec.LogAnalyticsWorkspaceName).To(Equal(to.Ptr("test/resourcegroups/test/")))
		Expect(upstreamSpec.PrivateCluster).To(Equal(to.Ptr(*clusterState.Properties.APIServerAccessProfile.EnablePrivateCluster)))
		Expect(upstreamSpec.PrivateDNSZone).To(Equal(to.Ptr(*clusterState.Properties.APIServerAccessProfile.PrivateDNSZone)))
		Expect(upstreamSpec.AuthorizedIPRanges).To(Equal(utils.ConvertToPointerOfSlice(clusterState.Properties.APIServerAccessProfile.AuthorizedIPRanges)))
	})

	It("should fail if azure client fails to get cluster", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(
			armcontainerservice.ManagedClustersClientGetResponse{
				ManagedCluster: *clusterState,
			}, errors.New("error"))

		_, err := handler.buildUpstreamClusterState(ctx, credentials, &aksConfig.Spec)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if kubernetes version is nil", func() {
		clusterState.Properties.KubernetesVersion = nil
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(
			armcontainerservice.ManagedClustersClientGetResponse{
				ManagedCluster: *clusterState,
			}, nil)

		_, err := handler.buildUpstreamClusterState(ctx, credentials, &aksConfig.Spec)
		Expect(err).To(HaveOccurred())
	})

	It("should succeed if dns prefix is nil", func() {
		clusterState.Properties.DNSPrefix = nil
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(
			armcontainerservice.ManagedClustersClientGetResponse{
				ManagedCluster: *clusterState,
			}, nil)

		_, err := handler.buildUpstreamClusterState(ctx, credentials, &aksConfig.Spec)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should succeed even if the monitoring addon is disabled", func() {
		clusterState.Properties.AddonProfiles["omsAgent"] = &armcontainerservice.ManagedClusterAddonProfile{
			Enabled: to.Ptr(false),
			Config:  nil,
		}
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), nil).Return(
			armcontainerservice.ManagedClustersClientGetResponse{
				ManagedCluster: *clusterState,
			}, nil)

		upstreamSpec, err := handler.buildUpstreamClusterState(ctx, credentials, &aksConfig.Spec)
		Expect(err).ToNot(HaveOccurred())
		Expect(*upstreamSpec.Monitoring).To(BeFalse())
		Expect(upstreamSpec.LogAnalyticsWorkspaceGroup).To(BeNil())
		Expect(upstreamSpec.LogAnalyticsWorkspaceGroup).To(BeNil())
	})
})

var _ = Describe("recordError", func() {
	var (
		aksConfig *aksv1.AKSClusterConfig
		handler   *Handler
	)

	BeforeEach(func() {
		aksConfig = &aksv1.AKSClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testrecorderror",
				Namespace: "default",
			},
			Spec: aksv1.AKSClusterConfigSpec{
				ResourceGroup: "test",
				ClusterName:   "test",
			},
		}

		Expect(cl.Create(ctx, aksConfig)).To(Succeed())
	})

	AfterEach(func() {
		Expect(test.CleanupAndWait(ctx, cl, aksConfig)).To(Succeed())
	})

	It("should return same conflict error when onChange returns a conflict error", func() {
		oldOutput := logrus.StandardLogger().Out
		buf := bytes.Buffer{}
		logrus.SetOutput(&buf)

		aksConfigUpdated := aksConfig.DeepCopy()
		Expect(cl.Update(ctx, aksConfigUpdated)).To(Succeed())

		var expectedErr error
		expectedConfig := &aksv1.AKSClusterConfig{}
		onChange := func(key string, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
			expectedErr = cl.Update(ctx, config)
			return expectedConfig, expectedErr
		}

		aksConfig.ResourceVersion = "1"
		handleFunction := handler.recordError(onChange)
		config, err := handleFunction("", aksConfig)

		Expect(config).To(Equal(expectedConfig))
		Expect(err).To(Equal(expectedErr))
		Expect("").To(Equal(string(buf.Bytes())))
		logrus.SetOutput(oldOutput)
	})

	It("should return same conflict error when onChange returns a conflict error and print a debug log for the error", func() {
		oldOutput := logrus.StandardLogger().Out
		buf := bytes.Buffer{}
		logrus.SetOutput(&buf)
		logrus.SetLevel(logrus.DebugLevel)

		aksConfigUpdated := aksConfig.DeepCopy()
		Expect(cl.Update(ctx, aksConfigUpdated)).To(Succeed())

		var expectedErr error
		expectedConfig := &aksv1.AKSClusterConfig{}
		onChange := func(key string, config *aksv1.AKSClusterConfig) (*aksv1.AKSClusterConfig, error) {
			expectedErr = cl.Update(ctx, config)
			return expectedConfig, expectedErr
		}

		aksConfig.ResourceVersion = "1"
		handleFunction := handler.recordError(onChange)
		config, err := handleFunction("", aksConfig)

		Expect(config).To(Equal(expectedConfig))
		Expect(err).To(MatchError(expectedErr))

		cleanLogOutput := strings.Replace(string(buf.Bytes()), `\"`, `"`, -1)
		Expect(strings.Contains(cleanLogOutput, err.Error())).To(BeTrue())
		logrus.SetOutput(oldOutput)
	})
})
