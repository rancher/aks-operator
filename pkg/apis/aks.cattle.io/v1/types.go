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
	Imported                    bool              `json:"imported" norman:"noupdate"`
	ResourceLocation            string            `json:"resourceLocation" norman:"noupdate"`
	ResourceGroup               string            `json:"resourceGroup" norman:"noupdate"`
	ClusterName                 string            `json:"clusterName" norman:"noupdate"`
	AzureCredentialSecret       string            `json:"azureCredentialSecret"`
	BaseURL                     *string           `json:"baseUrl"`
	AuthBaseURL                 *string           `json:"authBaseUrl"`
	NetworkPlugin               *string           `json:"networkPlugin"`
	VirtualNetworkResourceGroup *string           `json:"virtualNetworkResourceGroup"`
	VirtualNetwork              *string           `json:"virtualNetwork"`
	Subnet                      *string           `json:"subnet"`
	NetworkDNSServiceIP         *string           `json:"dnsServiceIp"`
	NetworkServiceCIDR          *string           `json:"serviceCidr"`
	NetworkDockerBridgeCIDR     *string           `json:"dockerBridgeCidr"`
	NetworkPodCIDR              *string           `json:"podCidr"`
	LoadBalancerSKU             *string           `json:"loadBalancerSku"`
	NetworkPolicy               *string           `json:"networkPolicy"`
	LinuxAdminUsername          *string           `json:"linuxAdminUsername,omitempty"`
	LinuxSSHPublicKey           *string           `json:"sshPublicKey,omitempty"`
	WindowsAdminUsername        *string           `json:"windowsAdminUsername,omitempty"`
	WindowsAdminPassword        *string           `json:"windowsAdminPassword,omitempty"`
	DNSPrefix                   *string           `json:"dnsPrefix,omitempty"`
	KubernetesVersion           *string           `json:"kubernetesVersion"`
	Tags                        map[string]string `json:"tags"`
	NodePools                   []AKSNodePool     `json:"nodePools"`
	PrivateCluster              *bool             `json:"privateCluster"`
	AuthorizedIPRanges          *[]string         `json:"authorizedIpRanges"`
}

type AKSClusterConfigStatus struct {
	Phase          string `json:"phase"`
	FailureMessage string `json:"failureMessage"`
}

type AKSNodePool struct {
	Name                *string   `json:"name,omitempty"`
	Count               *int32    `json:"count,omitempty"`
	MaxPods             *int32    `json:"maxPods,omitempty"`
	VMSize              string    `json:"vmSize,omitempty"`
	OsDiskSizeGB        *int32    `json:"osDiskSizeGB,omitempty"`
	OsDiskType          string    `json:"osDiskType,omitempty"`
	Mode                string    `json:"mode,omitempty"`
	OsType              string    `json:"osType,omitempty"`
	OrchestratorVersion *string   `json:"orchestratorVersion,omitempty"`
	AvailabilityZones   *[]string `json:"availabilityZones,omitempty"`
	MaxCount            *int32    `json:"maxCount,omitempty"`
	MinCount            *int32    `json:"minCount,omitempty"`
	EnableAutoScaling   *bool     `json:"enableAutoScaling,omitempty"`
}
