/*
Copyright © 2022 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apiserver/pkg/storage/names"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kubectl "github.com/rancher-sandbox/ele-testhelpers/kubectl"
	aksv1 "github.com/rancher/aks-operator/pkg/apis/aks.cattle.io/v1"
	e2eConfig "github.com/rancher/aks-operator/test/e2e/config"
	managementv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	runtimeconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(clientgoscheme.Scheme))
	utilruntime.Must(managementv3.AddToScheme(clientgoscheme.Scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(clientgoscheme.Scheme))
	utilruntime.Must(aksv1.AddToScheme(clientgoscheme.Scheme))
}

const (
	operatorName               = "aks-config-operator"
	crdChartName               = "aks-config-operator-crd"
	certManagerNamespace       = "cert-manager"
	certManagerName            = "cert-manager"
	certManagerCAInjectorName  = "cert-manager-cainjector"
	azureCredentialsSecretName = "azure-credentials"
	cattleSystemNamespace      = "cattle-system"
	rancherName                = "rancher"
)

// Test configuration
var (
	e2eCfg   *e2eConfig.E2EConfig
	cl       runtimeclient.Client
	ctx      = context.Background()
	crdNames = []string{
		"aksclusterconfigs.aks.cattle.io",
	}

	pollInterval = 10 * time.Second
	waitLong     = 15 * time.Minute
)

// Cluster Templates
var (
	//go:embed templates/*
	templates embed.FS

	clusterTemplates         = map[string]*aksv1.AKSClusterConfig{}
	basicClusterTemplateName = "basic-cluster"
)

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "aks-operator e2e test Suite")
}

var _ = BeforeSuite(func() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		Fail("config path can't be empty")
	}

	var err error
	e2eCfg, err = e2eConfig.ReadE2EConfig(configPath)
	Expect(err).ToNot(HaveOccurred())

	cfg, err := runtimeconfig.GetConfig()
	Expect(err).ToNot(HaveOccurred())

	cl, err = runtimeclient.New(cfg, runtimeclient.Options{})
	Expect(err).ToNot(HaveOccurred())

	By("Deploying rancher and cert-manager", func() {
		By("installing cert-manager", func() {
			if isDeploymentReady(certManagerNamespace, certManagerName) {
				By("already installed")
			} else {
				Expect(kubectl.RunHelmBinaryWithCustomErr(
					"-n",
					certManagerNamespace,
					"install",
					"--set",
					"installCRDs=true",
					"--create-namespace",
					certManagerNamespace,
					e2eCfg.CertManagerChartURL,
				)).To(Succeed())
				Eventually(func() bool {
					return isDeploymentReady(certManagerNamespace, certManagerName)
				}, 5*time.Minute, 2*time.Second).Should(BeTrue())
				Eventually(func() bool {
					return isDeploymentReady(certManagerNamespace, certManagerCAInjectorName)
				}, 5*time.Minute, 2*time.Second).Should(BeTrue())
			}
		})

		By("installing rancher", func() {
			if isDeploymentReady(cattleSystemNamespace, rancherName) {
				By("already installed")
			} else {
				Expect(kubectl.RunHelmBinaryWithCustomErr(
					"-n",
					cattleSystemNamespace,
					"install",
					"--set",
					"bootstrapPassword=admin",
					"--set",
					"replicas=1",
					"--set", fmt.Sprintf("hostname=%s.%s", e2eCfg.ExternalIP, e2eCfg.MagicDNS),
					"--create-namespace",
					rancherName,
					fmt.Sprintf(e2eCfg.RancherChartURL),
				)).To(Succeed())
				Eventually(func() bool {
					return isDeploymentReady(cattleSystemNamespace, rancherName)
				}, 5*time.Minute, 2*time.Second).Should(BeTrue())
			}
		})
	})

	By("Deploying aks operator CRD chart", func() {
		if isDeploymentReady(cattleSystemNamespace, operatorName) {
			By("already installed")
		} else {
			Expect(kubectl.RunHelmBinaryWithCustomErr(
				"-n",
				crdChartName,
				"install",
				"--create-namespace",
				"--set", "debug=true",
				operatorName,
				e2eCfg.CRDChart,
			)).To(Succeed())

			By("Waiting for CRDs to be created")
			Eventually(func() bool {
				for _, crdName := range crdNames {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					if err := cl.Get(ctx,
						runtimeclient.ObjectKey{
							Name: crdName,
						},
						crd,
					); err != nil {
						return false
					}
				}
				return true
			}, 5*time.Minute, 2*time.Second).Should(BeTrue())
		}
	})

	By("Deploying aks operator chart", func() {
		if isDeploymentReady(cattleSystemNamespace, operatorName) {
			By("already installed")
		} else {
			Expect(kubectl.RunHelmBinaryWithCustomErr(
				"-n",
				cattleSystemNamespace,
				"install",
				"--create-namespace",
				"--set", "debug=true",
				operatorName,
				e2eCfg.OperatorChart,
			)).To(Succeed())

			By("Waiting for aks operator deployment to be available")
			Eventually(func() bool {
				return isDeploymentReady(cattleSystemNamespace, operatorName)
			}, 5*time.Minute, 2*time.Second).Should(BeTrue())
		}
		// As we are not bootstrapping rancher in the tests (going to the first login page, setting new password and rancher-url)
		// We need to manually set this value, which is the same value you would get from doing the bootstrap
		setting := &managementv3.Setting{}
		Expect(cl.Get(ctx,
			runtimeclient.ObjectKey{
				Name: "server-url",
			},
			setting,
		)).To(Succeed())

		setting.Source = "env"
		setting.Value = fmt.Sprintf("https://%s.%s", e2eCfg.ExternalIP, e2eCfg.MagicDNS)

		Expect(cl.Update(ctx, setting)).To(Succeed())

	})

	By("Creating azure credentials secret", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      azureCredentialsSecretName,
				Namespace: "default",
			},
			Data: map[string][]byte{
				"azurecredentialConfig-clientId":       []byte(e2eCfg.AzureClientID),
				"azurecredentialConfig-clientSecret":   []byte(e2eCfg.AzureClientSecret),
				"azurecredentialConfig-subscriptionId": []byte(e2eCfg.AzureSubscriptionID),
			},
		}

		err := cl.Create(ctx, secret)
		if err != nil {
			fmt.Println(err)
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue())
		}
	})

	By("Reading cluster templates", func() {
		assets, err := templates.ReadDir("templates")
		Expect(err).ToNot(HaveOccurred())

		for _, asset := range assets {
			b, err := templates.ReadFile(path.Join("templates", asset.Name()))
			Expect(err).ToNot(HaveOccurred())

			aksCluster := &aksv1.AKSClusterConfig{}
			Expect(yaml.Unmarshal(b, aksCluster)).To(Succeed())

			name := strings.TrimSuffix(asset.Name(), ".yaml")
			generatedName := names.SimpleNameGenerator.GenerateName(name + "-")
			aksCluster.Name = generatedName
			aksCluster.Spec.ClusterName = generatedName
			aksCluster.Spec.ResourceGroup = e2eCfg.AzureResourceGroup

			clusterTemplates[name] = aksCluster
		}
	})
})

var _ = AfterSuite(func() {
	By("Creating artifact directory")

	if _, err := os.Stat(e2eCfg.ArtifactsDir); os.IsNotExist(err) {
		Expect(os.Mkdir(e2eCfg.ArtifactsDir, os.ModePerm)).To(Succeed())
	}

	By("Getting aks operator logs")

	podList := &corev1.PodList{}
	Expect(cl.List(ctx, podList, runtimeclient.MatchingLabels{
		"ke.cattle.io/operator": "aks",
	}, runtimeclient.InNamespace(cattleSystemNamespace),
	)).To(Succeed())

	for _, pod := range podList.Items {
		for _, container := range pod.Spec.Containers {
			output, err := kubectl.Run("logs", pod.Name, "-c", container.Name, "-n", pod.Namespace)
			Expect(err).ToNot(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(e2eCfg.ArtifactsDir, pod.Name+"-"+container.Name+".log"), redactSensitiveData([]byte(output)), 0644)).To(Succeed())
		}
	}

	By("Getting AKS Clusters")

	aksClusterList := &aksv1.AKSClusterConfigList{}
	Expect(cl.List(ctx, aksClusterList, &runtimeclient.ListOptions{
		Namespace: "default",
	})).To(Succeed())

	for _, aksCluster := range aksClusterList.Items {
		output, err := yaml.Marshal(aksCluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(e2eCfg.ArtifactsDir, aksCluster.Name+".yaml"), redactSensitiveData([]byte(output)), 0644)).To(Succeed())
	}

	By("Cleaning up AKS Clusters")

	for _, aksCluster := range aksClusterList.Items {
		Expect(cl.Delete(ctx, &aksCluster)).To(Succeed())
		Eventually(func() error {
			return cl.Get(ctx, runtimeclient.ObjectKey{
				Name:      aksCluster.Name,
				Namespace: aksCluster.Namespace,
			}, &aksv1.AKSClusterConfig{})
		}, waitLong, pollInterval).ShouldNot(Succeed())
	}
})

func isDeploymentReady(namespace, name string) bool {
	deployment := &appsv1.Deployment{}
	if err := cl.Get(ctx,
		runtimeclient.ObjectKey{
			Namespace: namespace,
			Name:      name,
		},
		deployment,
	); err != nil {
		return false
	}

	if deployment.Status.AvailableReplicas == *deployment.Spec.Replicas {
		return true
	}

	return false
}

func redactSensitiveData(input []byte) []byte {
	output := bytes.Replace(input, []byte(e2eCfg.AzureClientID), []byte("***"), -1)
	output = bytes.Replace(output, []byte(e2eCfg.AzureClientSecret), []byte("***"), -1)
	output = bytes.Replace(output, []byte(e2eCfg.AzureSubscriptionID), []byte("***"), -1)
	output = bytes.Replace(output, []byte(e2eCfg.AzureResourceGroup), []byte("***"), -1)
	return output
}
