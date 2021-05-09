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
	BaseURL                     *string           `json:"baseUrl" norman:"type=nullablestring"`
	AuthBaseURL                 *string           `json:"authBaseUrl" norman:"type=nullablestring"`
	NetworkPlugin               *string           `json:"networkPlugin" norman:"type=nullablestring"`
	VirtualNetworkResourceGroup *string           `json:"virtualNetworkResourceGroup" norman:"type=nullablestring"`
	VirtualNetwork              *string           `json:"virtualNetwork" norman:"type=nullablestring"`
	Subnet                      *string           `json:"subnet" norman:"type=nullablestring"`
	NetworkDNSServiceIP         *string           `json:"dnsServiceIp" norman:"type=nullablestring"`
	NetworkServiceCIDR          *string           `json:"serviceCidr" norman:"type=nullablestring"`
	NetworkDockerBridgeCIDR     *string           `json:"dockerBridgeCidr" norman:"type=nullablestring"`
	NetworkPodCIDR              *string           `json:"podCidr" norman:"type=nullablestring"`
	LoadBalancerSKU             *string           `json:"loadBalancerSku" norman:"type=nullablestring"`
	NetworkPolicy               *string           `json:"networkPolicy" norman:"type=nullablestring"`
	LinuxAdminUsername          *string           `json:"linuxAdminUsername,omitempty" norman:"type=nullablestring"`
	LinuxSSHPublicKey           *string           `json:"sshPublicKey,omitempty" norman:"type=nullablestring"`
	DNSPrefix                   *string           `json:"dnsPrefix,omitempty" norman:"type=nullablestring"`
	KubernetesVersion           *string           `json:"kubernetesVersion" norman:"type=nullablestring"`
	Tags                        map[string]string `json:"tags"`
	NodePools                   []AKSNodePool     `json:"nodePools"`
	PrivateCluster              *bool             `json:"privateCluster"`
	AuthorizedIPRanges          *[]string         `json:"authorizedIpRanges"`
	HTTPApplicationRouting      *bool             `json:"httpApplicationRouting"`
	Monitoring                  *bool             `json:"monitoring"`
	LogAnalyticsWorkspaceGroup  *string           `json:"logAnalyticsWorkspaceGroup"`
	LogAnalyticsWorkspaceName   *string           `json:"logAnalyticsWorkspaceName"`
}

type AKSClusterConfigStatus struct {
	Phase          string `json:"phase"`
	FailureMessage string `json:"failureMessage"`
}

type AKSNodePool struct {
	Name                *string   `json:"name,omitempty" norman:"type=nullablestring"`
	Count               *int32    `json:"count,omitempty"`
	MaxPods             *int32    `json:"maxPods,omitempty"`
	VMSize              string    `json:"vmSize,omitempty"`
	OsDiskSizeGB        *int32    `json:"osDiskSizeGB,omitempty"`
	OsDiskType          string    `json:"osDiskType,omitempty"`
	Mode                string    `json:"mode,omitempty"`
	OsType              string    `json:"osType,omitempty"`
	OrchestratorVersion *string   `json:"orchestratorVersion,omitempty" norman:"type=nullablestring"`
	AvailabilityZones   *[]string `json:"availabilityZones,omitempty"`
	MaxCount            *int32    `json:"maxCount,omitempty"`
	MinCount            *int32    `json:"minCount,omitempty"`
	EnableAutoScaling   *bool     `json:"enableAutoScaling,omitempty"`
}
