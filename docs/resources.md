#### Arguments for AKSClusterConfigSpec is the spec for a AKSClusterConfig resource

The following arguments are supported:

* `Imported` - (Optional) Is AKS cluster imported? Defaul: `false` (bool)
* `ResourceLocation` - (Required) The AKS resource location (string)
* `ResourceGroup` - (Required) The AKS resource group (string)
* `ClusterName` - (Required) The AKS cluster name (string)
* `AzureCredentialSecret` - (Required) The AKS Cloud Credential ID to use (string)
* `BaseURL` - (Optional) The AKS base url for Azure API (string)
* `AuthBaseUrl` - (Optional) The AKS authentication base url for Azure API (string)
* `NetworkPlugin` - (Optional) The AKS network plugin. Required if `imported=false` (string)
* `VirtualNetworkResourceGroup` - (Optional/Computed) The AKS virtual network resource group (string)
* `VirtualNetwork` - (Optional/Computed) The AKS virtual network (string)
* `Subnet` - (Optional/Computed) The AKS subnet (string)
* `NetworkDNSServiceIP` - (Optional/Computed) The AKS network dns service ip (string)
* `NetworkServiceCIDR` - (Optional/Computed) The AKS network service cidr (string)
* `NetworkDockerBridgeCIDR` - (Optional/Computed) The AKS network docker bridge cidr (string)
* `NetworkPodCIDR` - (Optional/Computed) The AKS network pod cidr (string)
* `NetworkPolicy` - (Optional/Computed) The AKS network policy (string)
* `LinuxAdminUsername` - (Optional/Computed) The AKS linux admin username (string)
* `LinuxSSHPublic_key` - (Optional/Computed) The AKS linux ssh public key (string)
* `DNSPrefix` - (Optional/ForceNew) The AKS dns prefix. Required if `imported=false` (string)
* `KubernetesVersion` - (Optional) The kubernetes master version. Required if `imported=false` (string)
* `Tags` - (Optional/Computed) The AKS cluster tags (map)
* `NodePools` - (Optional) The AKS nnode pools. Required if `imported=false` (list)
* `PrivateCluster` - (Optional/Computed) Is AKS cluster private? (bool)
* `AuthorizedIPRanges` - (Optional) The AKS authorized ip ranges (list)
* `HTTPApplicationRouting` - (Optional/Computed) Enable AKS http application routing? (bool)
* `Monitoring` - (Optional/Computed) Is AKS cluster monitoring enabled? (bool)
* `LogAnalyticsWorkspaceGroup` - (Optional/Computed) The AKS log analytics workspace group (string)
* `LogAnalyticsWorkspaceName` - (Optional/Computed) The AKS log analytics workspace name (string)


##### Arguments for AKSNodePool

* `Name` - (Required) The AKS node group name (string)
* `Count` - (Optional) The AKS node pool count. Default: `1` (int)
* `MaxPods` - (Optional) The AKS node pool max pods. Default: `110` (int)
* `VMSize` - (Optional/computed) The AKS node pool orchestrator version (string)
* `OsDiskSizeGB` - (Optional) The AKS node pool os disk size gb. Default: `128` (int)
* `OsDiskType` - (Optional) The AKS node pool os disk type. Default: `Managed` (string)
* `Mode` - (Optional) The AKS node group mode. Default: `System` (string)
* `OsType` - (Optional) The AKS node pool os type. Default: `Linux` (string)
* `OrchestratorVersion` - (Optional) The AKS node pool orchestrator version (string)
* `AvailabilityZones` - (Optional) The AKS node pool availability zones (list)
* `MaxSurge` - (Optional) The AKS node pool max surge (string), example value: `25%`
* `MaxCount` - (Optional) The AKS node pool max count. Required if `enable_auto_scaling=true` (int)
* `MinCount` - (Optional) The AKS node pool min count. Required if `enable_auto_scaling=true` (int)
* `EnableAutoScaling` - (Optional) Is AKS node pool auto scaling enabled? Default: `false` (bool)
* `NodeLabels` - (Optional) The AKS node pool labels (map)
* `NodeTaints` - (Optonal) The AKS node pool taints (list)

