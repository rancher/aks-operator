/*
Copyright 2017 The Kubernetes Authors.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type AKSClusterConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AKSClusterConfigSpec   `json:"spec"`
	Status AKSClusterConfigStatus `json:"status"`
}

// AKSClusterConfigSpec is the spec for a AKSClusterConfig resource
type AKSClusterConfigSpec struct {
	DisplayName                 string            `json:"displayName" norman:"noupdate"`
	Imported                    bool              `json:"imported" norman:"noupdate"`
	ResourceLocation            string            `json:"resourceLocation" norman:"noupdate"`
	ResourceGroup               string            `json:"resourceGroup" norman:"noupdate"`
	ClusterName                 string            `json:"clusterName" norman:"noupdate"`
	BaseURL                     string            `json:"baseUrl"`
	AuthBaseURL                 string            `json:"authBaseUrl"`
	SubscriptionID              string            `json:"subscriptionId"`
	TenantID                    string            `json:"tenantId"`
	AzureCredentialSecret       string            `json:"azureCredentialSecret"`
	NetworkPlugin               string            `json:"networkPlugin" norman:"noupdate"`
	VirtualNetworkResourceGroup string            `json:"virtualNetworkResourceGroup" norman:"noupdate"`
	VirtualNetwork              string            `json:"virtualNetwork" norman:"noupdate"`
	Subnet                      string            `json:"subnet" norman:"noupdate"`
	NetworkDNSServiceIP         string            `json:"dnsServiceIp" norman:"noupdate"`
	NetworkServiceCIDR          string            `json:"serviceCidr" norman:"noupdate"`
	NetworkDockerBridgeCIDR     string            `json:"dockerBridgeCidr" norman:"noupdate"`
	NetworkPodCIDR              string            `json:"podCidr" norman:"noupdate"`
	LoadBalancerSKU             string            `json:"loadBalancerSku" norman:"noupdate"`
	NetworkPolicy               string            `json:"networkPolicy" norman:"noupdate"`
	LinuxAdminUsername          string            `json:"adminUsername,omitempty" norman:"noupdate"`
	LinuxSSHPublicKeyContents   string            `json:"sshPublicKeyContents,omitempty" norman:"noupdate"`
	DNSPrefix                   *string           `json:"dnsPrefix,omitempty" norman:"noupdate"`
	KubernetesVersion           *string           `json:"kubernetesVersion"`
	Tags                        map[string]string `json:"tags"`
	NodePools                   []AKSNodePool     `json:"nodePools"`
	PrivateCluster              *bool             `json:"privateCluster" norman:"noupdate"`
	AuthorizedIPRanges          *[]string         `json:"authorizedIpRanges"`
}

type AKSClusterConfigStatus struct {
	Phase          string   `json:"phase"`
	FailureMessage string   `json:"failureMessage"`
	VirtualNetwork string   `json:"virtualNetwork"`
	Subnets        []string `json:"subnets"`
}

type AKSNodePool struct {
	Name                *string   `json:"name,omitempty"`
	Count               *int32    `json:"count,omitempty"`
	MaxPods             *int32    `json:"maxPods,omitempty"`
	VMSize              string    `json:"vmSize,omitempty" norman:"noupdate"`
	OsDiskSizeGB        *int32    `json:"osDiskSizeGB,omitempty" norman:"noupdate"`
	OsDiskType          string    `json:"osDiskType,omitempty" norman:"noupdate"`
	Mode                string    `json:"mode,omitempty" norman:"noupdate"`
	OsType              string    `json:"osType,omitempty" norman:"noupdate"`
	OrchestratorVersion *string   `json:"orchestratorVersion,omitempty"`
	AvailabilityZones   *[]string `json:"availabilityZones,omitempty"`
	MaxCount            *int32    `json:"maxCount,omitempty"`
	MinCount            *int32    `json:"minCount,omitempty"`
	EnableAutoScaling   *bool     `json:"enableAutoScaling,omitempty"`
}
