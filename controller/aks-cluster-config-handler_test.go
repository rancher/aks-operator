package controller

import (
	"errors"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2020-11-01/containerservice"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher/aks-operator/pkg/aks/services/mock_services"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	"github.com/rancher/aks-operator/pkg/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("importCluster", func() {
	var (
		handler           *Handler
		mockController    *gomock.Controller
		aksConfig         *aksv1.AKSClusterConfig
		clusterClientMock *mock_services.MockManagedClustersClientInterface
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
		kubeconfigYAML := `
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
		Expect(caSecret.OwnerReferences[0].UID).To(Equal(gotAKSConfig.UID))
		Expect(caSecret.OwnerReferences[0].Kind).To(Equal(aksClusterConfigKind))
		Expect(caSecret.OwnerReferences[0].APIVersion).To(Equal(aksv1.SchemeGroupVersion.String()))
		Expect(caSecret.Data["endpoint"]).To(Equal([]byte("https://test.com")))
		Expect(caSecret.Data["ca"]).To(Equal([]byte("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCnRlc3QKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=")))
	})

	It("should return error if something fails", func() {
		clusterClientMock.EXPECT().GetAccessProfile(gomock.Any(), aksConfig.Spec.ResourceGroup, aksConfig.Spec.ClusterName, "clusterAdmin").
			Return(containerservice.ManagedClusterAccessProfile{}, errors.New("error"))

		_, err := handler.importCluster(aksConfig)
		Expect(err).To(HaveOccurred())
	})
})
