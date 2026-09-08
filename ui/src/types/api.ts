// API types for Custom Resources

import {
  MutatingWebhookConfiguration,
  MutatingWebhookConfigurationList,
  ValidatingAdmissionPolicy,
  ValidatingAdmissionPolicyBinding,
  ValidatingAdmissionPolicyBindingList,
  ValidatingAdmissionPolicyList,
  ValidatingWebhookConfiguration,
  ValidatingWebhookConfigurationList,
} from 'kubernetes-types/admissionregistration/v1'
import {
  CustomResourceDefinition,
  CustomResourceDefinitionList,
} from 'kubernetes-types/apiextensions/v1'
import {
  DaemonSet,
  DaemonSetList,
  Deployment,
  DeploymentList,
  ReplicaSet,
  ReplicaSetList,
  StatefulSet,
  StatefulSetList,
} from 'kubernetes-types/apps/v1'
import {
  HorizontalPodAutoscaler,
  HorizontalPodAutoscalerList,
} from 'kubernetes-types/autoscaling/v2'
import { CronJob, CronJobList, Job, JobList } from 'kubernetes-types/batch/v1'
import { Lease, LeaseList } from 'kubernetes-types/coordination/v1'
import {
  ConfigMap,
  ConfigMapList,
  Endpoints,
  EndpointsList,
  Event,
  EventList,
  LimitRange,
  LimitRangeList,
  Namespace,
  NamespaceList,
  Node,
  PersistentVolume,
  PersistentVolumeClaim,
  PersistentVolumeClaimList,
  PersistentVolumeList,
  Pod,
  ResourceQuota,
  ResourceQuotaList,
  Secret,
  SecretList,
  Service,
  ServiceAccount,
  ServiceAccountList,
  ServiceList,
} from 'kubernetes-types/core/v1'
import { EndpointSlice, EndpointSliceList } from 'kubernetes-types/discovery/v1'
import {
  Ingress,
  IngressClass,
  IngressClassList,
  IngressList,
  NetworkPolicy,
  NetworkPolicyList,
} from 'kubernetes-types/networking/v1'
import { RuntimeClass, RuntimeClassList } from 'kubernetes-types/node/v1'
import {
  PodDisruptionBudget,
  PodDisruptionBudgetList,
} from 'kubernetes-types/policy/v1'
import {
  ClusterRole,
  ClusterRoleBinding,
  ClusterRoleBindingList,
  ClusterRoleList,
  Role as RawRole,
  RoleBinding,
  RoleBindingList,
  RoleList,
} from 'kubernetes-types/rbac/v1'
import {
  PriorityClass,
  PriorityClassList,
} from 'kubernetes-types/scheduling/v1'
import { StorageClass, StorageClassList } from 'kubernetes-types/storage/v1'

import type { ResourceType } from '@/lib/resource-metadata'

import { Gateway, GatewayClass, HTTPRoute } from './gateway'

export type { ResourceType } from '@/lib/resource-metadata'

export interface CustomResource {
  apiVersion: string
  kind: string
  metadata: {
    name: string
    namespace?: string
    creationTimestamp: string
    uid?: string
    resourceVersion?: string
    labels?: Record<string, string>
    annotations?: Record<string, string>
  }
  spec?: Record<string, unknown>
  status?: Record<string, unknown>
}

export interface CustomResourceList {
  apiVersion: string
  kind: string
  items: CustomResource[]
  metadata?: {
    continue?: string
    remainingItemCount?: number
  }
}

export interface DeploymentRelatedResource {
  events: Event[]
  pods: Pod[]
  services: Service[]
}

type listMetadataType = {
  continue?: string
  remainingItemCount?: number
}

// Define resource type mappings
export interface ResourcesTypeMap {
  leaderworkersets: {
    items: CustomResource[]
    metadata?: listMetadataType
  }
  pods: {
    items: PodWithMetrics[]
    metadata?: listMetadataType
  }
  deployments: DeploymentList
  statefulsets: StatefulSetList
  daemonsets: DaemonSetList
  jobs: JobList
  cronjobs: CronJobList
  services: ServiceList
  endpoints: EndpointsList
  endpointslices: EndpointSliceList
  resourcequotas: ResourceQuotaList
  limitranges: LimitRangeList
  gateways: {
    items: Gateway[]
    metadata?: listMetadataType
  }
  httproutes: {
    items: HTTPRoute[]
    metadata?: listMetadataType
  }
  gatewayclasses: {
    items: GatewayClass[]
    metadata?: listMetadataType
  }
  configmaps: ConfigMapList
  secrets: SecretList
  persistentvolumeclaims: PersistentVolumeClaimList
  ingresses: IngressList
  networkpolicies: NetworkPolicyList
  ingressclasses: IngressClassList
  namespaces: NamespaceList
  crds: CustomResourceDefinitionList
  crs: {
    items: CustomResource[]
    metadata?: listMetadataType
  }
  nodes: {
    items: NodeWithMetrics[]
    metadata?: listMetadataType
  }
  events: EventList
  persistentvolumes: PersistentVolumeList
  storageclasses: StorageClassList
  podmetrics: {
    items: PodMetrics[]
    metadata?: listMetadataType
  }
  replicasets: ReplicaSetList
  poddisruptionbudgets: PodDisruptionBudgetList
  priorityclasses: PriorityClassList
  runtimeclasses: RuntimeClassList
  leases: LeaseList
  mutatingwebhookconfigurations: MutatingWebhookConfigurationList
  validatingwebhookconfigurations: ValidatingWebhookConfigurationList
  validatingadmissionpolicies: ValidatingAdmissionPolicyList
  validatingadmissionpolicybindings: ValidatingAdmissionPolicyBindingList
  serviceaccounts: ServiceAccountList
  roles: RoleList
  rolebindings: RoleBindingList
  clusterroles: ClusterRoleList
  clusterrolebindings: ClusterRoleBindingList
  horizontalpodautoscalers: HorizontalPodAutoscalerList
}

export interface PodMetrics {
  metadata: {
    name: string
    namespace: string
    labels?: Record<string, string>
    annotations?: Record<string, string>
    creationTimestamp?: string
    uid?: string
    resourceVersion?: string
  }
  containers: {
    name: string // container name
    usage: {
      cpu: string // 214572390n
      memory: string // 2956516Ki
    }
  }[]
}

export type MetricsData = {
  cpuUsage?: number
  memoryUsage?: number
  cpuLimit?: number
  memoryLimit?: number
  cpuRequest?: number
  memoryRequest?: number
  gpuLimit?: number
  diskUsage?: number
  diskCapacity?: number
  pods?: number
  podsLimit?: number
}

export type PodWithMetrics = Pod & {
  metrics?: MetricsData
}

export type NodeWithMetrics = Node & {
  metrics?: MetricsData
}

export interface ResourceTypeMap {
  leaderworkersets: CustomResource
  pods: PodWithMetrics
  deployments: Deployment
  statefulsets: StatefulSet
  daemonsets: DaemonSet
  jobs: Job
  cronjobs: CronJob
  services: Service
  endpoints: Endpoints
  endpointslices: EndpointSlice
  resourcequotas: ResourceQuota
  limitranges: LimitRange
  gateways: Gateway
  httproutes: HTTPRoute
  gatewayclasses: GatewayClass
  configmaps: ConfigMap
  secrets: Secret
  persistentvolumeclaims: PersistentVolumeClaim
  ingresses: Ingress
  networkpolicies: NetworkPolicy
  ingressclasses: IngressClass
  namespaces: Namespace
  crds: CustomResourceDefinition
  crs: CustomResource
  nodes: NodeWithMetrics
  events: Event
  persistentvolumes: PersistentVolume
  storageclasses: StorageClass
  replicasets: ReplicaSet
  poddisruptionbudgets: PodDisruptionBudget
  priorityclasses: PriorityClass
  runtimeclasses: RuntimeClass
  leases: Lease
  mutatingwebhookconfigurations: MutatingWebhookConfiguration
  validatingwebhookconfigurations: ValidatingWebhookConfiguration
  validatingadmissionpolicies: ValidatingAdmissionPolicy
  validatingadmissionpolicybindings: ValidatingAdmissionPolicyBinding
  podmetrics: PodMetrics
  serviceaccounts: ServiceAccount
  roles: RawRole
  rolebindings: RoleBinding
  clusterroles: ClusterRole
  clusterrolebindings: ClusterRoleBinding
  horizontalpodautoscalers: HorizontalPodAutoscaler
}

export interface RecentEvent {
  type: string
  reason: string
  message: string
  involvedObjectKind: string
  involvedObjectName: string
  namespace?: string
  timestamp: string
}

export interface UsageDataPoint {
  timestamp: string
  value: number
}

export interface ResourceUsageHistory {
  cpu: UsageDataPoint[]
  memory: UsageDataPoint[]
  networkIn: UsageDataPoint[]
  networkOut: UsageDataPoint[]
  diskRead: UsageDataPoint[]
  diskWrite: UsageDataPoint[]
}

// Pod monitoring types
export interface PodMetrics {
  cpu: UsageDataPoint[]
  memory: UsageDataPoint[]
  networkIn?: UsageDataPoint[]
  networkOut?: UsageDataPoint[]
  diskRead?: UsageDataPoint[]
  diskWrite?: UsageDataPoint[]
  fallback?: boolean
}

export interface OverviewData {
  totalNodes: number
  readyNodes: number
  totalPods: number
  runningPods: number
  totalNamespaces: number
  totalServices: number
  prometheusEnabled: boolean
  resource: {
    cpu: {
      allocatable: number
      requested: number
      limited: number
    }
    memory: {
      allocatable: number
      requested: number
      limited: number
    }
  }
}

// GPU types
export interface GPUNodeInfo {
  nodeName: string
  capacity: number
  allocatable: number
  used: number
  free: number
  gpuType: string
  taints?: string[]
}

export interface GPUNamespaceStat {
  namespace: string
  gpuCount: number
}

export interface GPUModelStat {
  modelName: string
  gpuCount: number
}

export interface GPUModelRoleStat {
  modelName: string
  prefillNodes: number
  decodeNodes: number
}

export interface GPUOverview {
  summary: {
    totalNodes: number
    totalGPUs: number
    usedGPUs: number
    freeGPUs: number
    usagePercent: number
  }
  fullyFreeNodes: GPUNodeInfo[]
  untaintedFreeNodes: GPUNodeInfo[]
  taintedFreeNodes: GPUNodeInfo[]
  partialFreeNodes: GPUNodeInfo[]
  namespaceStats: GPUNamespaceStat[]
  modelStats: GPUModelStat[]
  noModelGPUCount: number
  modelRoleStats: GPUModelRoleStat[]
}

// Pagination types
export interface PaginationInfo {
  hasNextPage: boolean
  nextContinueToken?: string
  remainingItems?: number
}

export interface PaginationOptions {
  limit?: number
  continueToken?: string
}

// Pod current metrics types
export interface PodCurrentMetrics {
  podName: string
  namespace: string
  cpu: number // CPU cores
  memory: number // Memory in MB
}

export interface ImageTagInfo {
  name: string
  timestamp?: string
}

export interface RelatedResources {
  type: ResourceType
  name: string
  namespace?: string
  apiVersion?: string
}

export interface Cluster {
  id: number
  name: string
  description?: string
  version?: string
  config?: string
  enabled: boolean
  inCluster: boolean
  isDefault: boolean
  createdAt: string
  updatedAt: string
  prometheusURL?: string
  gpuResourceRules?: string[]
  error?: string
}

export interface OAuthProvider {
  id: number
  name: string
  clientId: string
  clientSecret: string
  authUrl?: string
  tokenUrl?: string
  userInfoUrl?: string
  scopes?: string
  issuer?: string
  usernameClaim?: string
  groupsClaim?: string
  allowedGroups?: string
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface RoleAssignment {
  id: number
  roleId: number
  subjectType: 'user' | 'group' | 'local_group'
  subject: string
  createdAt: string
  updatedAt: string
}

export interface Role {
  id: number
  name: string
  description?: string
  isSystem?: boolean
  clusters: string[]
  namespaces: string[]
  resources: string[]
  resourceNames?: string[]
  verbs: string[]
  allowProxy?: boolean
  proxyNamespaces?: string[]
  assignments?: RoleAssignment[]
  createdAt: string
  updatedAt: string
}

export interface UserItem {
  id: number
  username: string
  provider: string
  createdAt: string
  lastLoginAt?: string
  enabled?: boolean
  avatar_url?: string
  name?: string
  sub?: string
  roles?: Role[]
  groups?: UserGroup[]
}

export interface UserGroup {
  id: number
  name: string
  description?: string
  members: UserItem[]
  createdAt: string
  updatedAt: string
}

export interface FetchUserListResponse {
  users: UserItem[]
  total: number
  page: number
  size: number
}

export interface APIKey {
  id: number
  username: string
  apiKey: string
  lastLoginAt?: string
  createdAt: string
  updatedAt: string
  roles?: Role[]
}

// Resource History types
export interface ResourceHistory {
  id: number
  clusterName: string
  resourceType: string
  resourceName: string
  namespace: string
  operationType: string
  operationSource: string
  resourceYaml?: string
  previousYaml?: string
  success: boolean
  errorMessage: string
  operatorId: number
  operator: {
    username: string
    provider: string
    name?: string
  }
  createdAt: string
  updatedAt: string
}

export interface ResourceHistoryResponse {
  data: ResourceHistory[]
  pagination: {
    page: number
    pageSize: number
    total: number
    totalPages: number
    hasNextPage: boolean
    hasPrevPage: boolean
  }
}

export interface AuditLogResponse {
  data: ResourceHistory[]
  total: number
  page: number
  size: number
}

export interface AuditLogDetail {
  id: number
  resourceYaml: string
  previousYaml: string
}
export interface ResourceTemplate {
  id: number
  name: string
  description: string
  yaml: string
}
