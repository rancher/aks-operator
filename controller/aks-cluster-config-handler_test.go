package controller

import (
	"errors"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/Azure/azure-sdk-for-go/services/resources/mgmt/2019-10-01/resources"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher/aks-operator/pkg/aks"
	"github.com/rancher/aks-operator/pkg/aks/services/mock_services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	aksv1controllers "github.com/rancher/aks-operator/pkg/generated/controllers/aks.cattle.io"
	"github.com/rancher/aks-operator/pkg/test"
	"github.com/rancher/wrangler/pkg/generated/controllers/core"
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
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfig.Spec.ResourceGroup, aksConfig.Spec.ClusterName, "clusterAdmin").
			Return(containerservice.ManagedClusterAccessProfile{
				AccessProfile: &containerservice.AccessProfile{
					KubeConfig: to.ByteSlicePtr([]byte(kubeconfigYAML)),
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
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfig.Spec.ResourceGroup, aksConfig.Spec.ClusterName, "clusterAdmin").
			Return(containerservice.ManagedClusterAccessProfile{
				AccessProfile: &containerservice.AccessProfile{
					KubeConfig: to.ByteSlicePtr([]byte(kubeconfigYAML)),
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
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfig.Spec.ResourceGroup, aksConfig.Spec.ClusterName, "clusterAdmin").
			Return(containerservice.ManagedClusterAccessProfile{}, errors.New("error"))

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
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfigSpec.ResourceGroup, aksConfigSpec.ClusterName, "clusterAdmin").
			Return(containerservice.ManagedClusterAccessProfile{
				AccessProfile: &containerservice.AccessProfile{
					KubeConfig: to.ByteSlicePtr([]byte(kubeconfigYAML)),
				},
			}, nil)

		config, err := handler.getClusterKubeConfig(ctx, aksConfigSpec)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).ToNot(BeNil())
	})

	It("should return error if azure request fails", func() {
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfigSpec.ResourceGroup, aksConfigSpec.ClusterName, "clusterAdmin").
			Return(containerservice.ManagedClusterAccessProfile{}, errors.New("error"))

		_, err := handler.getClusterKubeConfig(ctx, aksConfigSpec)
		Expect(err).To(HaveOccurred())
	})

	It("should return error if config is failed to be created", func() {
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfigSpec.ResourceGroup, aksConfigSpec.ClusterName, "clusterAdmin").
			Return(containerservice.ManagedClusterAccessProfile{
				AccessProfile: &containerservice.AccessProfile{
					KubeConfig: to.ByteSlicePtr([]byte("invalid")),
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
				AuthBaseURL:           to.StringPtr("test"),
				BaseURL:               to.StringPtr("test"),
				ResourceLocation:      "test",
				ResourceGroup:         "test",
				ClusterName:           "test",
				KubernetesVersion:     to.StringPtr("test"),
				DNSPrefix:             to.StringPtr("test"),
				NodePools: []aksv1.AKSNodePool{
					{
						Name:         to.StringPtr("test1"),
						Count:        to.Int32Ptr(1),
						MaxPods:      to.Int32Ptr(1),
						VMSize:       "test",
						OsDiskSizeGB: to.Int32Ptr(1),
						OsDiskType:   "test",
						Mode:         "System",
						OsType:       "test",
					},
					{
						Name:         to.StringPtr("test2"),
						Count:        to.Int32Ptr(1),
						MaxPods:      to.Int32Ptr(1),
						VMSize:       "test",
						OsDiskSizeGB: to.Int32Ptr(1),
						OsDiskType:   "test",
						Mode:         "User",
						OsType:       "test",
					},
				},
				NetworkPlugin:       to.StringPtr(string(containerservice.Azure)),
				NetworkPolicy:       to.StringPtr(string(containerservice.NetworkPolicyAzure)),
				VirtualNetwork:      to.StringPtr("test"),
				Subnet:              to.StringPtr("test"),
				NetworkDNSServiceIP: to.StringPtr("test"),
				NetworkServiceCIDR:  to.StringPtr("test"),
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
		aksConfig.Spec.NodePools[0].Name = to.StringPtr("test")
		aksConfig.Spec.NodePools[1].Name = to.StringPtr("test")
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
		aksConfig.Spec.NetworkPlugin = to.StringPtr("invalid")
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if network policy is not azure or calico", func() {
		aksConfig.Spec.NetworkPolicy = to.StringPtr("invalid")
		Expect(handler.validateConfig(aksConfig)).NotTo(Succeed())
	})

	It("should fail if network policy is azure and network plugin is kubenet", func() {
		aksConfig.Spec.NetworkPlugin = to.StringPtr(string(containerservice.Kubenet))
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

	It("should fail if network pod cidr is empty", func() {
		aksConfig.Spec.NetworkServiceCIDR = nil
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
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		clusterClientMock = mock_services.NewMockManagedClustersClientInterface(mockController)
		resourceGroupClientMock = mock_services.NewMockResourceGroupsClientInterface(mockController)
		workplacesClientMock = mock_services.NewMockWorkplacesClientInterface(mockController)

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
				AuthBaseURL:           to.StringPtr("test"),
				BaseURL:               to.StringPtr("test"),
				ResourceLocation:      "test",
				ResourceGroup:         "test",
				ClusterName:           "test",
				KubernetesVersion:     to.StringPtr("test"),
				DNSPrefix:             to.StringPtr("test"),
				NodePools: []aksv1.AKSNodePool{
					{
						Name:         to.StringPtr("test"),
						Count:        to.Int32Ptr(1),
						MaxPods:      to.Int32Ptr(1),
						VMSize:       "test",
						OsDiskSizeGB: to.Int32Ptr(1),
						OsDiskType:   "test",
						Mode:         "System",
						OsType:       "test",
					},
				},
				NetworkPlugin:       to.StringPtr(string(containerservice.Azure)),
				NetworkPolicy:       to.StringPtr(string(containerservice.NetworkPolicyAzure)),
				VirtualNetwork:      to.StringPtr("test"),
				Subnet:              to.StringPtr("test"),
				NetworkDNSServiceIP: to.StringPtr("test"),
				NetworkServiceCIDR:  to.StringPtr("test"),
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
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(containerservice.ManagedCluster{
			Response: autorest.Response{
				Response: &http.Response{
					StatusCode: 404,
				},
			},
		}, nil)

		clusterClientMock.EXPECT().CreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(containerservice.ManagedClustersCreateOrUpdateFuture{},
			nil)

		resourceGroupClientMock.EXPECT().CheckExistence(gomock.Any(), gomock.Any()).Return(autorest.Response{
			Response: &http.Response{
				StatusCode: 404,
			},
		}, nil)

		resourceGroupClientMock.EXPECT().CreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any()).Return(resources.Group{}, nil)

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
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(containerservice.ManagedCluster{
			Response: autorest.Response{
				Response: &http.Response{
					StatusCode: 200,
				},
			},
		}, nil)

		_, err := handler.createCluster(aksConfig)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if resource group failed to be created", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(containerservice.ManagedCluster{
			Response: autorest.Response{
				Response: &http.Response{
					StatusCode: 404,
				},
			},
		}, nil)

		resourceGroupClientMock.EXPECT().CheckExistence(gomock.Any(), gomock.Any()).Return(autorest.Response{
			Response: &http.Response{
				StatusCode: 404,
			},
		}, nil)

		resourceGroupClientMock.EXPECT().CreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any()).Return(resources.Group{}, errors.New("error"))

		_, err := handler.createCluster(aksConfig)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if cluster failed to be created", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(containerservice.ManagedCluster{
			Response: autorest.Response{
				Response: &http.Response{
					StatusCode: 404,
				},
			},
		}, nil)

		clusterClientMock.EXPECT().CreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(containerservice.ManagedClustersCreateOrUpdateFuture{},
			errors.New("error"))

		resourceGroupClientMock.EXPECT().CheckExistence(gomock.Any(), gomock.Any()).Return(autorest.Response{
			Response: &http.Response{
				StatusCode: 404,
			},
		}, nil)

		resourceGroupClientMock.EXPECT().CreateOrUpdate(gomock.Any(), gomock.Any(), gomock.Any()).Return(resources.Group{}, nil)

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
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(
			containerservice.ManagedCluster{
				ManagedClusterProperties: &containerservice.ManagedClusterProperties{
					ProvisioningState: to.StringPtr(ClusterStatusSucceeded),
				},
			},
			nil)

		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfig.Spec.ResourceGroup, aksConfig.Spec.ClusterName, "clusterAdmin").
			Return(containerservice.ManagedClusterAccessProfile{
				AccessProfile: &containerservice.AccessProfile{
					KubeConfig: to.ByteSlicePtr([]byte(kubeconfigYAML)),
				},
			},
				nil)

		gotAKSConfig, err := handler.waitForCluster(aksConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(gotAKSConfig.Status.Phase).To(Equal(aksConfigActivePhase))
	})

	It("should continue waiting for cluster if it's still creating", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(
			containerservice.ManagedCluster{
				ManagedClusterProperties: &containerservice.ManagedClusterProperties{
					ProvisioningState: to.StringPtr("Creating"),
				},
			},
			nil)

		_, err := handler.waitForCluster(aksConfig)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should fail if azure client failed to get cluster", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(containerservice.ManagedCluster{}, errors.New("error"))

		_, err := handler.waitForCluster(aksConfig)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if cluster creation failed", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(
			containerservice.ManagedCluster{
				ManagedClusterProperties: &containerservice.ManagedClusterProperties{
					ProvisioningState: to.StringPtr(ClusterStatusFailed),
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

		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(
			containerservice.ManagedCluster{
				ManagedClusterProperties: &containerservice.ManagedClusterProperties{
					ProvisioningState: to.StringPtr(ClusterStatusSucceeded),
				},
			},
			nil)

		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfig.Spec.ResourceGroup, aksConfig.Spec.ClusterName, "clusterAdmin").
			Return(containerservice.ManagedClusterAccessProfile{
				AccessProfile: &containerservice.AccessProfile{
					KubeConfig: to.ByteSlicePtr([]byte(kubeconfigYAML)),
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
		clusterState      *containerservice.ManagedCluster
	)

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		clusterClientMock = mock_services.NewMockManagedClustersClientInterface(mockController)

		credentials = &aks.Credentials{
			AuthBaseURL: to.StringPtr("test"),
			BaseURL:     to.StringPtr("test"),
		}

		aksConfig = &aksv1.AKSClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: aksv1.AKSClusterConfigSpec{},
		}

		clusterState = &containerservice.ManagedCluster{
			ManagedClusterProperties: &containerservice.ManagedClusterProperties{
				KubernetesVersion: to.StringPtr("test"),
				DNSPrefix:         to.StringPtr("test"),
				AgentPoolProfiles: &[]containerservice.ManagedClusterAgentPoolProfile{
					{
						Name:                to.StringPtr("test"),
						Count:               to.Int32Ptr(1),
						MaxPods:             to.Int32Ptr(1),
						VMSize:              containerservice.StandardA1,
						OsDiskSizeGB:        to.Int32Ptr(1),
						OsType:              containerservice.Linux,
						Mode:                containerservice.User,
						OrchestratorVersion: to.StringPtr("test"),
						AvailabilityZones:   to.StringSlicePtr([]string{"test"}),
						EnableAutoScaling:   to.BoolPtr(true),
						MaxCount:            to.Int32Ptr(1),
						MinCount:            to.Int32Ptr(1),
						VnetSubnetID:        to.StringPtr("test"),
						NodeLabels:          *to.StringMapPtr(map[string]string{"test": "test"}),
						NodeTaints:          to.StringSlicePtr([]string{"test"}),
						UpgradeSettings: &containerservice.AgentPoolUpgradeSettings{
							MaxSurge: to.StringPtr("test"),
						},
					},
				},
				NetworkProfile: &containerservice.NetworkProfile{
					NetworkPlugin:   containerservice.Azure,
					DNSServiceIP:    to.StringPtr("test"),
					ServiceCidr:     to.StringPtr("test"),
					NetworkPolicy:   containerservice.NetworkPolicyAzure,
					PodCidr:         to.StringPtr("test"),
					LoadBalancerSku: containerservice.Standard,
				},
				LinuxProfile: &containerservice.LinuxProfile{
					AdminUsername: to.StringPtr("test"),
					SSH: &containerservice.SSHConfiguration{
						PublicKeys: &[]containerservice.SSHPublicKey{
							{
								KeyData: to.StringPtr("test"),
							},
						},
					},
				},
				AddonProfiles: map[string]*containerservice.ManagedClusterAddonProfile{
					"httpApplicationRouting": {
						Enabled: to.BoolPtr(true),
					},
					"omsAgent": {
						Enabled: to.BoolPtr(true),
						Config: map[string]*string{
							"logAnalyticsWorkspaceResourceID": to.StringPtr("/workspaces/test/resourcegroups/test/"),
						},
					},
				},
				APIServerAccessProfile: &containerservice.ManagedClusterAPIServerAccessProfile{
					EnablePrivateCluster: to.BoolPtr(true),
					AuthorizedIPRanges:   to.StringSlicePtr([]string{"test"}),
					PrivateDNSZone:       to.StringPtr("test-private-dns-zone-id"),
				},
			},
			Tags: *to.StringMapPtr(map[string]string{"test": "test"}),
		}

		handler = &Handler{
			azureClients: azureClients{
				clustersClient: clusterClientMock,
			},
		}
	})

	It("should build upstream cluster state", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(*clusterState, nil)

		upstreamSpec, err := handler.buildUpstreamClusterState(ctx, credentials, &aksConfig.Spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(upstreamSpec).NotTo(BeNil())

		Expect(upstreamSpec.KubernetesVersion).To(Equal(clusterState.KubernetesVersion))
		Expect(upstreamSpec.DNSPrefix).To(Equal(clusterState.DNSPrefix))
		Expect(upstreamSpec.Tags).To(Equal(to.StringMap(clusterState.Tags)))
		Expect(upstreamSpec.NodePools).To(HaveLen(1))
		nodePools := *clusterState.AgentPoolProfiles
		Expect(upstreamSpec.NodePools[0].Name).To(Equal(nodePools[0].Name))
		Expect(upstreamSpec.NodePools[0].Count).To(Equal(nodePools[0].Count))
		Expect(upstreamSpec.NodePools[0].MaxPods).To(Equal(nodePools[0].MaxPods))
		Expect(upstreamSpec.NodePools[0].VMSize).To(Equal(string(nodePools[0].VMSize)))
		Expect(upstreamSpec.NodePools[0].OsDiskSizeGB).To(Equal(nodePools[0].OsDiskSizeGB))
		Expect(upstreamSpec.NodePools[0].OsType).To(Equal(string(nodePools[0].OsType)))
		Expect(upstreamSpec.NodePools[0].Mode).To(Equal(string(nodePools[0].Mode)))
		Expect(upstreamSpec.NodePools[0].OrchestratorVersion).To(Equal(nodePools[0].OrchestratorVersion))
		Expect(upstreamSpec.NodePools[0].AvailabilityZones).To(Equal(nodePools[0].AvailabilityZones))
		Expect(upstreamSpec.NodePools[0].EnableAutoScaling).To(Equal(nodePools[0].EnableAutoScaling))
		Expect(upstreamSpec.NodePools[0].MaxCount).To(Equal(nodePools[0].MaxCount))
		Expect(upstreamSpec.NodePools[0].MinCount).To(Equal(nodePools[0].MinCount))
		Expect(upstreamSpec.NodePools[0].VnetSubnetID).To(Equal(nodePools[0].VnetSubnetID))
		Expect(upstreamSpec.NodePools[0].NodeLabels).To(Equal(nodePools[0].NodeLabels))
		Expect(upstreamSpec.NodePools[0].NodeTaints).To(Equal(nodePools[0].NodeTaints))
		Expect(upstreamSpec.NodePools[0].MaxSurge).To(Equal(nodePools[0].UpgradeSettings.MaxSurge))
		Expect(upstreamSpec.NetworkPlugin).To(Equal(to.StringPtr(string(clusterState.NetworkProfile.NetworkPlugin))))
		Expect(upstreamSpec.NetworkDNSServiceIP).To(Equal(clusterState.NetworkProfile.DNSServiceIP))
		Expect(upstreamSpec.NetworkServiceCIDR).To(Equal(clusterState.NetworkProfile.ServiceCidr))
		Expect(upstreamSpec.NetworkPolicy).To(Equal(to.StringPtr(string(clusterState.NetworkProfile.NetworkPolicy))))
		Expect(upstreamSpec.NetworkPodCIDR).To(Equal(clusterState.NetworkProfile.PodCidr))
		Expect(upstreamSpec.LoadBalancerSKU).To(Equal(to.StringPtr(string(clusterState.NetworkProfile.LoadBalancerSku))))
		Expect(upstreamSpec.LinuxAdminUsername).To(Equal(clusterState.LinuxProfile.AdminUsername))
		Expect(upstreamSpec.LinuxSSHPublicKey).To(Equal(to.StringPtr(*(*clusterState.LinuxProfile.SSH.PublicKeys)[0].KeyData)))
		Expect(upstreamSpec.HTTPApplicationRouting).To(Equal(to.BoolPtr(*clusterState.AddonProfiles["httpApplicationRouting"].Enabled)))
		Expect(upstreamSpec.Monitoring).To(Equal(to.BoolPtr(*clusterState.AddonProfiles["omsAgent"].Enabled)))
		Expect(upstreamSpec.LogAnalyticsWorkspaceGroup).To(Equal(to.StringPtr("test")))
		Expect(upstreamSpec.LogAnalyticsWorkspaceName).To(Equal(to.StringPtr("test/resourcegroups/test/")))
		Expect(upstreamSpec.PrivateCluster).To(Equal(to.BoolPtr(*clusterState.APIServerAccessProfile.EnablePrivateCluster)))
		Expect(upstreamSpec.PrivateDNSZone).To(Equal(to.StringPtr(*clusterState.APIServerAccessProfile.PrivateDNSZone)))
		Expect(upstreamSpec.AuthorizedIPRanges).To(Equal(clusterState.APIServerAccessProfile.AuthorizedIPRanges))
	})

	It("should fail if azure client fails to get cluster", func() {
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(*clusterState, errors.New("error"))

		_, err := handler.buildUpstreamClusterState(ctx, credentials, &aksConfig.Spec)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if kubernetes version is nil", func() {
		clusterState.KubernetesVersion = nil
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(*clusterState, nil)

		_, err := handler.buildUpstreamClusterState(ctx, credentials, &aksConfig.Spec)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if dns prefix is nil", func() {
		clusterState.DNSPrefix = nil
		clusterClientMock.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(*clusterState, nil)

		_, err := handler.buildUpstreamClusterState(ctx, credentials, &aksConfig.Spec)
		Expect(err).ToNot(HaveOccurred())
	})
})
