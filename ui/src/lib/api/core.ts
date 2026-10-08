import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Pod } from 'kubernetes-types/core/v1'

import {
  ImageTagInfo,
  RelatedResources,
  ResourceHistoryResponse,
  ResourcesTypeMap,
  ResourceTemplate,
  ResourceType,
  ResourceTypeMap,
} from '@/types/api'
import { getResourceQueryKey } from '@/lib/resource-metadata'
import {
  applyResourceWatchDeltas,
  ResourceWatchDelta,
  sortWatchedResources,
  WatchableResource,
} from '@/lib/resource-watch'

import { API_BASE_URL, apiClient } from '../api-client'
import {
  appendCurrentClusterParam,
  CURRENT_CLUSTER_HEADER_KEY,
  getClusterQueryKey,
} from '../current-cluster'
import { withSubPath } from '../subpath'
import { fetchAPI } from './shared'

type ResourcesItems<T extends ResourceType> = ResourcesTypeMap[T]['items']

export const fetchResources = <T>(
  resource: string,
  namespace?: string,
  opts?: {
    limit?: number
    continueToken?: string
    labelSelector?: string
    fieldSelector?: string
    reduce?: boolean
    namespaces?: string[]
    cluster?: string
  }
): Promise<T> => {
  let endpoint = namespace ? `/${resource}/${namespace}` : `/${resource}`
  const params = new URLSearchParams()

  if (opts?.limit) {
    params.append('limit', opts.limit.toString())
  }
  if (opts?.continueToken) {
    params.append('continue', opts.continueToken)
  }
  if (opts?.labelSelector) {
    params.append('labelSelector', opts.labelSelector)
  }
  if (opts?.fieldSelector) {
    params.append('fieldSelector', opts.fieldSelector)
  }
  if (opts?.reduce) {
    params.append('reduce', 'true')
  }
  if (opts?.namespaces?.length) {
    params.append('namespaces', opts.namespaces.join(','))
  }

  if (params.toString()) {
    endpoint += `?${params.toString()}`
  }

  return apiClient.get<T>(
    endpoint,
    opts?.cluster
      ? { headers: { [CURRENT_CLUSTER_HEADER_KEY]: opts.cluster } }
      : undefined
  )
}

// Search API types
export interface SearchResult {
  id: string
  name: string
  namespace?: string
  resourceType: string
  createdAt: string
}

export interface SearchResponse {
  results: SearchResult[]
  total: number
}

export interface CRDVersionSummary {
  name: string
  served: boolean
  storage: boolean
  additionalPrinterColumns?: Array<{
    name: string
    type: string
    jsonPath: string
    description?: string
    format?: string
    priority?: number
  }>
}

export interface CRDSummary {
  name: string
  group: string
  kind: string
  scope: 'Cluster' | 'Namespaced'
  createdAt?: string
  established: boolean
  versions: CRDVersionSummary[]
}

interface CRDSummaryResponse {
  items: CRDSummary[]
}

export const useCRDSummaries = (options?: { enabled?: boolean }) => {
  return useQuery({
    queryKey: getClusterQueryKey('crd-summaries'),
    queryFn: () => fetchAPI<CRDSummaryResponse>('/crds/_all/_summaries'),
    select: (data) => data.items,
    enabled: options?.enabled ?? true,
    staleTime: 30000,
  })
}

// Global search API
export const globalSearch = async (
  query: string,
  options?: {
    limit?: number
    namespace?: string
  }
): Promise<SearchResponse> => {
  if (query.length < 2) {
    return { results: [], total: 0 }
  }

  const params = new URLSearchParams({
    q: query,
    limit: String(options?.limit || 50),
  })

  if (options?.namespace) {
    params.append('namespace', options.namespace)
  }

  const endpoint = `/search?${params.toString()}`
  return fetchAPI<SearchResponse>(endpoint)
}
// Scale deployment API
export const scaleDeployment = async (
  namespace: string,
  name: string,
  replicas: number
): Promise<{ message: string; deployment: unknown; replicas: number }> => {
  const endpoint = `/deployments/${namespace}/${name}/scale`
  const response = await apiClient.put<{
    message: string
    deployment: unknown
    replicas: number
  }>(endpoint, {
    replicas,
  })

  return response
}

// Node operation APIs
export const drainNode = async (
  nodeName: string,
  options: {
    force: boolean
    gracePeriod: number
    deleteLocalData: boolean
    ignoreDaemonsets: boolean
  }
): Promise<{
  message: string
  node: string
  pods: number
  warnings?: string | string[]
}> => {
  const endpoint = `/nodes/_all/${nodeName}/drain`
  const response = await apiClient.post<{
    message: string
    node: string
    pods: number
    warnings?: string | string[]
  }>(endpoint, options)

  return response
}

export const cordonNode = async (
  nodeName: string
): Promise<{ message: string; node: string; unschedulable: boolean }> => {
  const endpoint = `/nodes/_all/${nodeName}/cordon`
  const response = await apiClient.post<{
    message: string
    node: string
    unschedulable: boolean
  }>(endpoint)

  return response
}

export const uncordonNode = async (
  nodeName: string
): Promise<{ message: string; node: string; unschedulable: boolean }> => {
  const endpoint = `/nodes/_all/${nodeName}/uncordon`
  const response = await apiClient.post<{
    message: string
    node: string
    unschedulable: boolean
  }>(endpoint)

  return response
}

export const taintNode = async (
  nodeName: string,
  taint: {
    key: string
    value: string
    effect: 'NoSchedule' | 'PreferNoSchedule' | 'NoExecute'
  }
): Promise<{ message: string; node: string; taint: unknown }> => {
  const endpoint = `/nodes/_all/${nodeName}/taint`
  const response = await apiClient.post<{
    message: string
    node: string
    taint: unknown
  }>(endpoint, taint)

  return response
}

export const untaintNode = async (
  nodeName: string,
  key: string
): Promise<{ message: string; node: string; removedTaintKey: string }> => {
  const endpoint = `/nodes/_all/${nodeName}/untaint`
  const response = await apiClient.post<{
    message: string
    node: string
    removedTaintKey: string
  }>(endpoint, { key })

  return response
}

export const restoreNodeScheduling = async (
  nodeName: string
): Promise<{
  message: string
  node: string
  removedTaints: number
  uncordoned: boolean
}> => {
  const endpoint = `/nodes/_all/${nodeName}/restore`
  const response = await apiClient.post<{
    message: string
    node: string
    removedTaints: number
    uncordoned: boolean
  }>(endpoint)

  return response
}

export const updateResource = async <T extends ResourceType>(
  resource: T,
  name: string,
  namespace: string | undefined,
  body: ResourceTypeMap[T]
): Promise<void> => {
  const endpoint = `/${resource}/${namespace || '_all'}/${name}`
  await apiClient.put(`${endpoint}`, body)
}

export const resizePod = async (
  namespace: string,
  name: string,
  body: Partial<Pod>
): Promise<void> => {
  const endpoint = `/pods/${namespace || '_all'}/${name}/resize`
  await apiClient.patch(`${endpoint}`, body)
}

type DeepPartial<T> = T extends object
  ? {
      [P in keyof T]?: DeepPartial<T[P]>
    }
  : T
export const patchResource = async <T extends ResourceType>(
  resource: T,
  name: string,
  namespace: string | undefined,
  body: DeepPartial<ResourceTypeMap[T]>
): Promise<void> => {
  const endpoint = `/${resource}/${namespace || '_all'}/${name}`
  await apiClient.patch(`${endpoint}`, body)
}

export const createResource = async <T extends ResourceType>(
  resource: T,
  namespace: string | undefined,
  body: ResourceTypeMap[T]
): Promise<ResourceTypeMap[T]> => {
  const endpoint = `/${resource}/${namespace || '_all'}`
  return await apiClient.post<ResourceTypeMap[T]>(`${endpoint}`, body)
}

export const deleteResource = async <T extends ResourceType>(
  resource: T,
  name: string,
  namespace: string | undefined,
  opts?: {
    force?: boolean
    wait?: boolean
  }
): Promise<void> => {
  const params = new URLSearchParams()
  if (opts?.force) {
    params.append('force', 'true')
  }
  if (opts?.wait === false) {
    params.append('wait', 'false')
  }
  const endpoint = `/${resource}/${namespace || '_all'}/${name}?${params.toString()}`
  await apiClient.delete(endpoint)
}

// Apply resource from YAML
export interface ApplyResourceRequest {
  yaml: string
  createOnly?: boolean
}

export interface ApplyResourceResponse {
  message: string
  kind: string
  name: string
  namespace?: string
  resources: Array<{
    kind: string
    name: string
    namespace?: string
  }>
  count: number
}

export const applyResource = async (
  yaml: string,
  createOnly?: boolean
): Promise<ApplyResourceResponse> => {
  return await apiClient.post<ApplyResourceResponse>('/resources/apply', {
    yaml,
    ...(createOnly && { createOnly: true }),
  })
}

export const useResourcesEvents = <T extends ResourceType>(
  resource: T,
  name: string,
  namespace?: string
) => {
  return useQuery({
    queryKey: getClusterQueryKey('resource-events', resource, namespace, name),
    queryFn: () => {
      const endpoint =
        '/events/resources?' +
        new URLSearchParams({
          resource: resource,
          name: name,
          namespace: namespace || '',
        }).toString()
      return fetchAPI<ResourcesTypeMap['events']>(endpoint)
    },
    select: (data: ResourcesTypeMap['events']): ResourcesItems<'events'> =>
      data.items,
    placeholderData: (prevData) => prevData,
  })
}

export const useResources = <T extends ResourceType>(
  resource: T,
  namespace?: string,
  options?: {
    staleTime?: number
    limit?: number
    labelSelector?: string
    fieldSelector?: string
    refreshInterval?: number
    disable?: boolean
    reduce?: boolean
    namespaces?: string[]
    cluster?: string
  }
) => {
  return useQuery({
    queryKey: getClusterQueryKey(
      resource,
      namespace,
      options?.limit,
      options?.labelSelector,
      options?.fieldSelector,
      options?.namespaces?.join(','),
      options?.cluster
    ),
    queryFn: () => {
      return fetchResources<ResourcesTypeMap[T]>(resource, namespace, {
        limit: options?.limit,
        continueToken: undefined,
        labelSelector: options?.labelSelector,
        fieldSelector: options?.fieldSelector,
        reduce: options?.reduce,
        namespaces: options?.namespaces,
        cluster: options?.cluster,
      })
    },
    enabled: !options?.disable,
    select: (data: ResourcesTypeMap[T]): ResourcesItems<T> => data.items,
    placeholderData: (prevData) => prevData,
    refetchInterval: options?.refreshInterval || 0,
    staleTime: options?.staleTime || (resource === 'crds' ? 30000 : 1000),
  })
}

// Hook: SSE watch for resource lists (initial snapshot + ADDED/MODIFIED/DELETED)
const RESOURCE_WATCH_CONNECT_TIMEOUT_MS = 5000

export function useResourcesWatch<T extends ResourceType>(
  resource: T,
  namespace?: string,
  options?: {
    labelSelector?: string
    fieldSelector?: string
    reduce?: boolean
    enabled?: boolean
    cluster?: string | null
    namespaces?: string[]
  }
) {
  const [data, setData] = useState<ResourcesItems<T> | undefined>(undefined)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  const [isConnected, setIsConnected] = useState(false)
  const [isUnsupported, setIsUnsupported] = useState(false)
  const eventSourceRef = useRef<EventSource | null>(null)
  const dataRef = useRef<ResourceTypeMap[T][]>([])
  const pendingDeltasRef = useRef<
    ResourceWatchDelta<ResourceTypeMap[T] & WatchableResource>[]
  >([])
  const flushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const connectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const activeUrlRef = useRef<string | null>(null)
  const hasSnapshotRef = useRef(false)
  const connectionErrorCountRef = useRef(0)

  const buildUrl = useCallback(() => {
    const ns = namespace || '_all'
    const params = new URLSearchParams()
    if (options?.reduce !== false) params.append('reduce', 'true')
    if (options?.labelSelector)
      params.append('labelSelector', options.labelSelector)
    if (options?.fieldSelector)
      params.append('fieldSelector', options.fieldSelector)
    if (options?.namespaces?.length)
      params.append('namespaces', options.namespaces.join(','))
    appendCurrentClusterParam(params, options?.cluster)
    return withSubPath(
      `${API_BASE_URL}/${resource}/${ns}/_watch?${params.toString()}`
    )
  }, [
    resource,
    namespace,
    options?.reduce,
    options?.labelSelector,
    options?.fieldSelector,
    options?.cluster,
    options?.namespaces,
  ])

  const clearConnectTimer = useCallback(() => {
    if (connectTimerRef.current) {
      clearTimeout(connectTimerRef.current)
      connectTimerRef.current = null
    }
  }, [])

  const disconnect = useCallback(() => {
    clearConnectTimer()
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
      eventSourceRef.current = null
    }
    if (flushTimerRef.current) {
      clearTimeout(flushTimerRef.current)
      flushTimerRef.current = null
    }
    pendingDeltasRef.current = []
  }, [clearConnectTimer])

  const flushDeltas = useCallback(() => {
    flushTimerRef.current = null
    if (pendingDeltasRef.current.length === 0) return
    const next = applyResourceWatchDeltas(
      dataRef.current as (ResourceTypeMap[T] & WatchableResource)[],
      pendingDeltasRef.current
    )
    pendingDeltasRef.current = []
    dataRef.current = next as ResourceTypeMap[T][]
    setData(next as ResourcesItems<T>)
  }, [])

  const enqueueDelta = useCallback(
    (
      type: ResourceWatchDelta<ResourceTypeMap[T] & WatchableResource>['type'],
      serializedObject: string
    ) => {
      const object = JSON.parse(serializedObject) as ResourceTypeMap[T] &
        WatchableResource
      pendingDeltasRef.current.push({ type, object })
      if (!flushTimerRef.current) {
        flushTimerRef.current = setTimeout(flushDeltas, 100)
      }
    },
    [flushDeltas]
  )

  const connect = useCallback(() => {
    disconnect()
    if (options?.enabled === false) return
    const url = buildUrl()
    if (activeUrlRef.current !== url) {
      activeUrlRef.current = url
      dataRef.current = []
      hasSnapshotRef.current = false
      setData(undefined)
    }
    connectionErrorCountRef.current = 0
    setError(null)
    setIsUnsupported(false)
    setIsConnected(false)
    setIsLoading(true)

    try {
      const markUnavailable = (es: EventSource) => {
        clearConnectTimer()
        setError(new Error('Resource watch connection is unavailable'))
        setIsLoading(false)
        setIsConnected(false)
        setIsUnsupported(true)
        es.close()
        if (eventSourceRef.current === es) {
          eventSourceRef.current = null
        }
      }

      const openEventSource = () => {
        let es: EventSource
        try {
          es = new EventSource(url, { withCredentials: true })
        } catch (err) {
          clearConnectTimer()
          setError(
            err instanceof Error
              ? err
              : new Error('Resource watch connection is unavailable')
          )
          setIsLoading(false)
          setIsConnected(false)
          setIsUnsupported(true)
          return
        }
        eventSourceRef.current = es
        let attemptReportedError = false
        const isCurrentConnection = () => eventSourceRef.current === es
        const fallbackThreshold = () => (hasSnapshotRef.current ? 5 : 3)

        const armConnectTimer = () => {
          clearConnectTimer()
          connectTimerRef.current = setTimeout(() => {
            connectTimerRef.current = null
            if (!isCurrentConnection()) return

            if (!attemptReportedError) {
              connectionErrorCountRef.current += 1
            }
            if (connectionErrorCountRef.current >= fallbackThreshold()) {
              markUnavailable(es)
              return
            }

            es.close()
            if (eventSourceRef.current === es) {
              eventSourceRef.current = null
            }
            openEventSource()
          }, RESOURCE_WATCH_CONNECT_TIMEOUT_MS)
        }

        armConnectTimer()

        es.onopen = () => {
          if (!isCurrentConnection()) return
          clearConnectTimer()
        }

        es.addEventListener('snapshot', (e: MessageEvent<string>) => {
          if (!isCurrentConnection()) return
          clearConnectTimer()
          const payload = JSON.parse(e.data) as {
            items?: (ResourceTypeMap[T] & WatchableResource)[]
          }
          if (flushTimerRef.current) {
            clearTimeout(flushTimerRef.current)
            flushTimerRef.current = null
          }
          pendingDeltasRef.current = []
          const items = sortWatchedResources([...(payload.items || [])])
          hasSnapshotRef.current = true
          connectionErrorCountRef.current = 0
          dataRef.current = items as ResourceTypeMap[T][]
          setData(items as ResourcesItems<T>)
          setError(null)
          setIsLoading(false)
          setIsConnected(false)
        })

        es.addEventListener('added', (e: MessageEvent<string>) => {
          if (!isCurrentConnection()) return
          enqueueDelta('added', e.data)
        })
        es.addEventListener('modified', (e: MessageEvent<string>) => {
          if (!isCurrentConnection()) return
          enqueueDelta('modified', e.data)
        })
        es.addEventListener('deleted', (e: MessageEvent<string>) => {
          if (!isCurrentConnection()) return
          enqueueDelta('deleted', e.data)
        })

        es.addEventListener('ready', () => {
          if (!isCurrentConnection()) return
          clearConnectTimer()
          connectionErrorCountRef.current = 0
          setError(null)
          setIsLoading(false)
          setIsConnected(true)
        })

        es.addEventListener('watch-error', (e: MessageEvent<string>) => {
          if (!isCurrentConnection()) return
          const payload = JSON.parse(e.data) as {
            error?: string
            fatal?: boolean
          }
          setError(new Error(payload.error || 'Resource watch failed'))
          setIsLoading(false)
          setIsConnected(false)
          if (payload.fatal) {
            clearConnectTimer()
            setIsUnsupported(true)
            es.close()
            if (eventSourceRef.current === es) {
              eventSourceRef.current = null
            }
          }
        })

        es.onerror = () => {
          if (!isCurrentConnection()) return
          setIsConnected(false)
          attemptReportedError = true
          connectionErrorCountRef.current += 1
          if (connectionErrorCountRef.current >= fallbackThreshold()) {
            markUnavailable(es)
            return
          }
          // CLOSED is also reported for transient proxy/server disconnects.
          // Give every failed attempt the same two-second reconnect window
          // instead of permanently falling back after the first HTTP failure.
          armConnectTimer()
        }
      }

      openEventSource()
    } catch (err) {
      clearConnectTimer()
      if (err instanceof Error) setError(err)
      setIsLoading(false)
      setIsConnected(false)
      setIsUnsupported(true)
    }
  }, [buildUrl, clearConnectTimer, disconnect, enqueueDelta, options?.enabled])

  const refetch = useCallback(() => {
    connect()
  }, [connect])

  useEffect(() => {
    if (options?.enabled === false) {
      disconnect()
      setIsConnected(false)
      setIsLoading(false)
      setIsUnsupported(false)
      return
    }
    connect()
    return () => {
      disconnect()
    }
  }, [connect, disconnect, options?.enabled])

  return {
    data,
    isLoading,
    error,
    isConnected,
    isUnsupported,
    refetch,
    stop: disconnect,
  }
}

export const fetchResource = <T>(
  resource: string,
  name: string,
  namespace?: string
): Promise<T> => {
  const endpoint = namespace
    ? `/${resource}/${namespace}/${name}`
    : `/${resource}/${name}`
  return fetchAPI<T>(endpoint)
}
export const useResource = <T extends keyof ResourceTypeMap>(
  resource: T,
  name: string,
  namespace?: string,
  options?: { staleTime?: number; refreshInterval?: number }
) => {
  const ns = namespace || '_all'
  return useQuery({
    queryKey: getResourceQueryKey(resource, ns, name),
    queryFn: () => {
      return fetchResource<ResourceTypeMap[T]>(resource, name, ns)
    },
    refetchOnWindowFocus: 'always',
    refetchInterval: options?.refreshInterval || 0, // Default to no auto-refresh
    placeholderData: (prevData) => prevData,
    staleTime: options?.staleTime || 1000,
  })
}
// Pod describe API
export const fetchDescribe = async (
  resourceType: ResourceType,
  name: string,
  namespace?: string
): Promise<{ result: string }> => {
  const endpoint = `/${resourceType}/${namespace ?? '_all'}/${name}/describe`
  return fetchAPI<{ result: string }>(endpoint)
}

export const useDescribe = (
  resourceType: ResourceType,
  name: string,
  namespace?: string,
  options?: { staleTime?: number; enabled?: boolean }
) => {
  return useQuery({
    queryKey: getClusterQueryKey(resourceType, name, namespace, 'describe'),
    queryFn: () => fetchDescribe(resourceType, name, namespace),
    enabled: (options?.enabled ?? true) && !!name,
    staleTime: options?.staleTime || 0,
    retry: 0,
  })
}
export interface FileInfo {
  name: string
  isDir: boolean
  size: string
  modTime: string
  mode: string
  uid: string
  gid: string
}

export const podListFiles = async (
  namespace: string,
  podName: string,
  container: string,
  path: string,
  options?: RequestInit
): Promise<FileInfo[]> => {
  const params = new URLSearchParams({
    container,
    path,
  })
  return apiClient.get<FileInfo[]>(
    `/pods/${namespace}/${podName}/files?${params.toString()}`,
    options
  )
}

export const podDownloadFile = (
  namespace: string,
  podName: string,
  container: string,
  path: string
) => {
  const params = new URLSearchParams({
    container,
    path,
  })
  appendCurrentClusterParam(params)
  const url = withSubPath(
    `${API_BASE_URL}/pods/${namespace}/${podName}/files/download?${params.toString()}`
  )
  window.open(url, '_blank')
}

export const podPreviewFile = (
  namespace: string,
  podName: string,
  container: string,
  path: string
) => {
  const params = new URLSearchParams({
    container,
    path,
  })
  appendCurrentClusterParam(params)
  const url = withSubPath(
    `${API_BASE_URL}/pods/${namespace}/${podName}/files/preview?${params.toString()}`
  )
  window.open(url, '_blank')
}

export const podUploadFile = async (
  namespace: string,
  podName: string,
  container: string,
  path: string,
  file: File
): Promise<void> => {
  const formData = new FormData()
  formData.append('file', file)
  const params = new URLSearchParams({
    container,
    path,
  })

  await apiClient.put(
    `/pods/${namespace}/${podName}/files/upload?${params.toString()}`,
    formData
  )
}

export const fetchTemplates = async (): Promise<ResourceTemplate[]> => {
  return fetchAPI<ResourceTemplate[]>('/templates/')
}

export const createTemplate = async (
  data: Omit<ResourceTemplate, 'id'>
): Promise<ResourceTemplate> => {
  return apiClient.post<ResourceTemplate>('/admin/templates/', data)
}

export const updateTemplate = async (
  id: number,
  data: Partial<ResourceTemplate>
): Promise<ResourceTemplate> => {
  return apiClient.put<ResourceTemplate>(`/admin/templates/${id}`, data)
}

export const deleteTemplate = async (id: number): Promise<void> => {
  await apiClient.delete(`/admin/templates/${id}`)
}

export const useTemplates = (options?: { staleTime?: number }) => {
  return useQuery({
    queryKey: ['templates'],
    queryFn: fetchTemplates,
    staleTime: options?.staleTime || 30000,
  })
}
export async function getImageTags(image: string): Promise<ImageTagInfo[]> {
  if (!image) return []
  const resp = await apiClient.get<ImageTagInfo[]>(
    `/image/tags?image=${encodeURIComponent(image)}`
  )
  return resp
}

export function useImageTags(image: string, options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: ['image-tags', image],
    queryFn: () => getImageTags(image),
    enabled: !!image && (options?.enabled ?? true),
    staleTime: 60 * 1000, // 1 min
    placeholderData: (prev) => prev,
  })
}

export async function getRelatedResources(
  resource: ResourceType,
  name: string,
  namespace?: string
) {
  const resp = await apiClient.get<RelatedResources[]>(
    `/${resource}/${namespace ? namespace : '_all'}/${name}/related`
  )
  return resp
}

export function useRelatedResources(
  resource: ResourceType,
  name: string,
  namespace?: string
) {
  return useQuery({
    queryKey: getClusterQueryKey(
      'related-resources',
      resource,
      name,
      namespace
    ),
    queryFn: () => getRelatedResources(resource, name, namespace),
    staleTime: 60 * 1000, // 1 min
    placeholderData: (prev) => prev,
  })
}
// Resource History API
export const fetchResourceHistory = (
  resourceType: string,
  namespace: string,
  name: string,
  page: number = 1,
  pageSize: number = 10
): Promise<ResourceHistoryResponse> => {
  const endpoint = `/${resourceType}/${namespace}/${name}/history?page=${page}&pageSize=${pageSize}`
  return fetchAPI<ResourceHistoryResponse>(endpoint)
}

export const useResourceHistory = (
  resourceType: string,
  namespace: string,
  name: string,
  page: number = 1,
  pageSize: number = 10,
  options?: { enabled?: boolean; staleTime?: number }
) => {
  return useQuery({
    queryKey: getClusterQueryKey(
      'resource-history',
      resourceType,
      namespace,
      name,
      page,
      pageSize
    ),
    queryFn: () =>
      fetchResourceHistory(resourceType, namespace, name, page, pageSize),
    enabled: options?.enabled ?? true,
    staleTime: options?.staleTime || 30000, // 30 seconds cache
  })
}
export const usePodFiles = (
  namespace: string,
  podName: string,
  container: string,
  path: string,
  options?: { enabled?: boolean }
) => {
  return useQuery({
    queryKey: getClusterQueryKey(
      'pod-files',
      namespace,
      podName,
      container,
      path
    ),
    queryFn: () => podListFiles(namespace, podName, container, path),
    enabled: options?.enabled !== false,
    staleTime: 10000, // 10 seconds cache
  })
}
