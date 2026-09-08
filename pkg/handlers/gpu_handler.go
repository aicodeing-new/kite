package handlers

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zxh326/kite/pkg/cluster"
	"github.com/zxh326/kite/pkg/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var automaticGPUResourcePattern = regexp.MustCompile(`(?i)(^gpu$|^gpu\.[^/]+/|/(?:[^/]+-)?v?gpu(?:$|\.)|/mig-[^/]+$)`)

type gpuResourceMatcher struct {
	included map[string]struct{}
	excluded map[string]struct{}
}

func newGPUResourceMatcher(rules []string) gpuResourceMatcher {
	matcher := gpuResourceMatcher{
		included: make(map[string]struct{}),
		excluded: make(map[string]struct{}),
	}
	for _, rawRule := range rules {
		rule := strings.ToLower(strings.TrimSpace(rawRule))
		if rule == "" {
			continue
		}
		if excluded := strings.TrimPrefix(rule, "!"); excluded != rule {
			if excluded != "" {
				matcher.excluded[excluded] = struct{}{}
			}
			continue
		}
		matcher.included[rule] = struct{}{}
	}
	return matcher
}

func (m gpuResourceMatcher) matches(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if _, excluded := m.excluded[name]; excluded {
		return false
	}
	if _, included := m.included[name]; included {
		return true
	}
	return automaticGPUResourcePattern.MatchString(name)
}

func gpuResourceMatcherForCluster(clusterName string) gpuResourceMatcher {
	if clusterName == "" || model.DB == nil {
		return newGPUResourceMatcher(nil)
	}
	configuredCluster, err := model.GetClusterByName(clusterName)
	if err != nil {
		// GPU overview remains useful with automatic detection even if its optional
		// per-cluster overrides cannot be loaded.
		klog.Warningf("Failed to load GPU resource rules for cluster %s: %v", clusterName, err)
		return newGPUResourceMatcher(nil)
	}
	return newGPUResourceMatcher(configuredCluster.GPUResourceRules)
}

// GPUNodeInfo 存储节点 GPU 信息
type GPUNodeInfo struct {
	NodeName    string   `json:"nodeName"`
	Capacity    int64    `json:"capacity"`
	Allocatable int64    `json:"allocatable"`
	Used        int64    `json:"used"`
	Free        int64    `json:"free"`
	GPUType     string   `json:"gpuType"`
	Taints      []string `json:"taints,omitempty"`
}

// GPUNamespaceStat 按 Namespace 的 GPU 使用统计
type GPUNamespaceStat struct {
	Namespace string `json:"namespace"`
	GPUCount  int64  `json:"gpuCount"`
}

// GPUModelStat 按模型的 GPU 使用统计
type GPUModelStat struct {
	ModelName string `json:"modelName"`
	GPUCount  int64  `json:"gpuCount"`
}

// GPUModelRoleStat 按模型的 Prefill/Decode 角色统计
type GPUModelRoleStat struct {
	ModelName    string `json:"modelName"`
	PrefillNodes int64  `json:"prefillNodes"`
	DecodeNodes  int64  `json:"decodeNodes"`
}

// GPUOverview GPU 资源概览
type GPUOverview struct {
	Summary struct {
		TotalNodes   int     `json:"totalNodes"`
		TotalGPUs    int64   `json:"totalGPUs"`
		UsedGPUs     int64   `json:"usedGPUs"`
		FreeGPUs     int64   `json:"freeGPUs"`
		UsagePercent float64 `json:"usagePercent"`
	} `json:"summary"`
	FullyFreeNodes     []GPUNodeInfo      `json:"fullyFreeNodes"`
	UntaintedFreeNodes []GPUNodeInfo      `json:"untaintedFreeNodes"`
	TaintedFreeNodes   []GPUNodeInfo      `json:"taintedFreeNodes"`
	PartialFreeNodes   []GPUNodeInfo      `json:"partialFreeNodes"`
	NamespaceStats     []GPUNamespaceStat `json:"namespaceStats"`
	ModelStats         []GPUModelStat     `json:"modelStats"`
	NoModelGPUCount    int64              `json:"noModelGPUCount"`
	ModelRoleStats     []GPUModelRoleStat `json:"modelRoleStats"`
}

// GetGPUOverview 获取 GPU 资源概览
func GetGPUOverview(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	cs := c.MustGet("cluster").(*cluster.ClientSet)
	matcher := gpuResourceMatcherForCluster(cs.Name)

	// 获取 GPU 节点信息
	nodes, err := getGPUNodes(ctx, cs, matcher)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get GPU nodes: %v", err)})
		return
	}

	if len(nodes) == 0 {
		// 返回空数据而不是错误
		c.JSON(http.StatusOK, GPUOverview{
			Summary: struct {
				TotalNodes   int     `json:"totalNodes"`
				TotalGPUs    int64   `json:"totalGPUs"`
				UsedGPUs     int64   `json:"usedGPUs"`
				FreeGPUs     int64   `json:"freeGPUs"`
				UsagePercent float64 `json:"usagePercent"`
			}{},
			FullyFreeNodes:     []GPUNodeInfo{},
			UntaintedFreeNodes: []GPUNodeInfo{},
			TaintedFreeNodes:   []GPUNodeInfo{},
			PartialFreeNodes:   []GPUNodeInfo{},
			NamespaceStats:     []GPUNamespaceStat{},
			ModelStats:         []GPUModelStat{},
			ModelRoleStats:     []GPUModelRoleStat{},
		})
		return
	}

	// 获取 Pod GPU 使用情况
	nodeGPUUsage, err := getGPUUsageFromPods(ctx, cs, matcher)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get GPU usage from pods: %v", err)})
		return
	}

	// 更新节点使用情况
	for i := range nodes {
		nodes[i].Used = nodeGPUUsage[nodes[i].NodeName]
		nodes[i].Free = nodes[i].Capacity - nodes[i].Used
	}

	// 获取 LWS 统计信息
	namespaceStats, modelStats, noModelCount, roleStatsMap := getLWSStats(ctx, cs, matcher)

	// 生成概览数据
	overview := buildGPUOverview(nodes, namespaceStats, modelStats, noModelCount, roleStatsMap)

	c.JSON(http.StatusOK, overview)
}

// getGPUNodes 获取所有 GPU 节点
func getGPUNodes(ctx context.Context, cs *cluster.ClientSet, matcher gpuResourceMatcher) ([]GPUNodeInfo, error) {
	var nodeList corev1.NodeList
	if err := cs.K8sClient.List(ctx, &nodeList); err != nil {
		return nil, err
	}

	var gpuNodes []GPUNodeInfo

	for _, node := range nodeList.Items {
		var gpuCapacity int64
		var gpuAllocatable int64
		var gpuTypes []string

		// 检查 GPU 资源
		for resourceName, qty := range node.Status.Capacity {
			if !matcher.matches(string(resourceName)) || qty.Value() <= 0 {
				continue
			}
			gpuCapacity += qty.Value()
			if allocQty, ok := node.Status.Allocatable[resourceName]; ok {
				gpuAllocatable += allocQty.Value()
			}
			gpuTypes = append(gpuTypes, string(resourceName))
		}

		if gpuCapacity > 0 {
			sort.Strings(gpuTypes)
			// 收集污点信息
			var taints []string
			for _, taint := range node.Spec.Taints {
				taints = append(taints, fmt.Sprintf("%s:%s", taint.Key, taint.Effect))
			}

			gpuNodes = append(gpuNodes, GPUNodeInfo{
				NodeName:    node.Name,
				Capacity:    gpuCapacity,
				Allocatable: gpuAllocatable,
				GPUType:     strings.Join(gpuTypes, ","),
				Taints:      taints,
			})
		}
	}

	return gpuNodes, nil
}

// getGPUUsageFromPods 从 Pod 获取 GPU 使用情况
func getGPUUsageFromPods(ctx context.Context, cs *cluster.ClientSet, matcher gpuResourceMatcher) (map[string]int64, error) {
	var podList corev1.PodList
	if err := cs.K8sClient.List(ctx, &podList); err != nil {
		return nil, err
	}

	nodeGPUUsage := make(map[string]int64)

	for _, pod := range podList.Items {
		// 只统计 Running 和 Pending 的 Pod
		if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
			continue
		}

		if pod.Spec.NodeName == "" {
			continue
		}

		var totalGPU int64
		for _, container := range pod.Spec.Containers {
			containerGPU := gpuQuantityFromResourceList(container.Resources.Requests, matcher)
			if containerGPU == 0 {
				containerGPU = gpuQuantityFromResourceList(container.Resources.Limits, matcher)
			}
			totalGPU += containerGPU
		}

		if totalGPU > 0 {
			nodeGPUUsage[pod.Spec.NodeName] += totalGPU
		}
	}

	return nodeGPUUsage, nil
}

func gpuQuantityFromResourceList(resources corev1.ResourceList, matcher gpuResourceMatcher) int64 {
	var total int64
	for resourceName, quantity := range resources {
		if matcher.matches(string(resourceName)) {
			total += quantity.Value()
		}
	}
	return total
}

// getLWSStats 从 LWS (LeaderWorkerSet) 获取统计信息
func getLWSStats(ctx context.Context, cs *cluster.ClientSet, matcher gpuResourceMatcher) (map[string]int64, map[string]int64, int64, map[string]*GPUModelRoleStat) {
	namespaceStats := make(map[string]int64)
	modelStats := make(map[string]int64)
	roleStatsMap := make(map[string]*GPUModelRoleStat)
	var noModelCount int64

	var lwsList unstructured.UnstructuredList
	lwsList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "leaderworkerset.x-k8s.io",
		Version: "v1",
		Kind:    "LeaderWorkerSetList",
	})

	if err := cs.K8sClient.List(ctx, &lwsList, &client.ListOptions{}); err != nil {
		// LWS 可能不存在，不返回错误
		return namespaceStats, modelStats, noModelCount, roleStatsMap
	}

	for _, item := range lwsList.Items {
		namespace := item.GetNamespace()

		// 获取 replicas
		replicas, _, _ := unstructured.NestedInt64(item.Object, "spec", "replicas")

		// 获取 size
		size, found, _ := unstructured.NestedInt64(item.Object, "spec", "leaderWorkerTemplate", "size")
		if !found {
			size = 1
		}

		// 获取 leader GPU
		leaderGPU := extractGPUFromContainers(item.Object, []string{"spec", "leaderWorkerTemplate", "leaderTemplate", "spec", "containers"}, matcher)

		// 获取 worker GPU
		workerGPU := extractGPUFromContainers(item.Object, []string{"spec", "leaderWorkerTemplate", "workerTemplate", "spec", "containers"}, matcher)

		// 计算总 GPU: replicas × (leaderGPU + workerGPU × (size-1))
		totalGPU := replicas * (leaderGPU + workerGPU*(size-1))

		if totalGPU > 0 {
			// 统计 namespace
			namespaceStats[namespace] += totalGPU

			// 获取模型名称
			modelName := ""
			leaderLabels, found, _ := unstructured.NestedStringMap(item.Object, "spec", "leaderWorkerTemplate", "leaderTemplate", "metadata", "labels")
			if found && leaderLabels != nil {
				if val, ok := leaderLabels["model.magikcompute.ai/name"]; ok {
					modelName = val
				}
			}

			if modelName != "" {
				modelStats[modelName] += totalGPU
			} else {
				noModelCount += totalGPU
			}

			// 统计 Prefill/Decode 角色（仅当 role label 存在时）
			// role 在 leaderTemplate.metadata.labels 中，值如 Prefill/Decode（不区分大小写）
			role := ""
			if leaderLabels != nil {
				if val, ok := leaderLabels["model.magikcompute.ai/role"]; ok {
					role = strings.ToLower(val)
				}
			}
			// 也检查 LWS 自身 metadata.labels 作为 fallback
			if role == "" {
				lwsLabels := item.GetLabels()
				if lwsLabels != nil {
					if val, ok := lwsLabels["model.magikcompute.ai/role"]; ok {
						role = strings.ToLower(val)
					}
				}
			}
			// 机器数：若 workerTemplate 无容器（即 worker 实际不存在），有效 size 为 1（仅 leader）
			workerContainers, _, _ := unstructured.NestedSlice(item.Object, "spec", "leaderWorkerTemplate", "workerTemplate", "spec", "containers")
			effectiveSize := size
			if len(workerContainers) == 0 {
				effectiveSize = 1
			}
			machines := effectiveSize * replicas
			if role != "" && modelName != "" && machines > 0 {
				if _, exists := roleStatsMap[modelName]; !exists {
					roleStatsMap[modelName] = &GPUModelRoleStat{ModelName: modelName}
				}
				switch role {
				case "prefill":
					roleStatsMap[modelName].PrefillNodes += machines
				case "decode":
					roleStatsMap[modelName].DecodeNodes += machines
				}
			}
		}
	}

	return namespaceStats, modelStats, noModelCount, roleStatsMap
}

// extractGPUFromContainers 从容器配置中提取 GPU 数量
func extractGPUFromContainers(obj map[string]interface{}, path []string, matcher gpuResourceMatcher) int64 {
	containers, _, _ := unstructured.NestedSlice(obj, path...)
	var totalGPU int64

	for _, container := range containers {
		containerMap, ok := container.(map[string]interface{})
		if !ok {
			continue
		}

		var containerGPU int64
		// 检查 requests
		if requests, found, _ := unstructured.NestedMap(containerMap, "resources", "requests"); found {
			containerGPU = gpuQuantityFromUnstructuredMap(requests, matcher)
		}

		// 如果没有 requests，检查 limits
		if containerGPU == 0 {
			if limits, found, _ := unstructured.NestedMap(containerMap, "resources", "limits"); found {
				containerGPU = gpuQuantityFromUnstructuredMap(limits, matcher)
			}
		}
		totalGPU += containerGPU
	}

	return totalGPU
}

func gpuQuantityFromUnstructuredMap(resources map[string]interface{}, matcher gpuResourceMatcher) int64 {
	var total int64
	for resourceName, value := range resources {
		if !matcher.matches(resourceName) {
			continue
		}
		switch gpuValue := value.(type) {
		case int64:
			total += gpuValue
		case int:
			total += int64(gpuValue)
		case float64:
			total += int64(gpuValue)
		case string:
			var gpu int64
			if _, err := fmt.Sscanf(gpuValue, "%d", &gpu); err == nil {
				total += gpu
			}
		}
	}
	return total
}

// buildGPUOverview 构建 GPU 概览数据
func buildGPUOverview(nodes []GPUNodeInfo, namespaceStats, modelStats map[string]int64, noModelCount int64, roleStatsMap map[string]*GPUModelRoleStat) GPUOverview {
	var overview GPUOverview

	// 排序节点
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].NodeName < nodes[j].NodeName
	})

	// 统计信息
	var totalGPUs, usedGPUs, freeGPUs int64
	var fullyFreeNodes, untaintedFreeNodes, taintedFreeNodes, partialFreeNodes []GPUNodeInfo

	for _, node := range nodes {
		totalGPUs += node.Capacity
		usedGPUs += node.Used
		freeGPUs += node.Free

		if node.Free > 0 {
			if node.Used == 0 {
				fullyFreeNodes = append(fullyFreeNodes, node)
				if len(node.Taints) > 0 {
					taintedFreeNodes = append(taintedFreeNodes, node)
				} else {
					untaintedFreeNodes = append(untaintedFreeNodes, node)
				}
			} else {
				partialFreeNodes = append(partialFreeNodes, node)
			}
		}
	}

	overview.Summary.TotalNodes = len(nodes)
	overview.Summary.TotalGPUs = totalGPUs
	overview.Summary.UsedGPUs = usedGPUs
	overview.Summary.FreeGPUs = freeGPUs
	if totalGPUs > 0 {
		overview.Summary.UsagePercent = float64(usedGPUs) / float64(totalGPUs) * 100
	}

	overview.FullyFreeNodes = fullyFreeNodes
	if overview.FullyFreeNodes == nil {
		overview.FullyFreeNodes = []GPUNodeInfo{}
	}

	overview.UntaintedFreeNodes = untaintedFreeNodes
	if overview.UntaintedFreeNodes == nil {
		overview.UntaintedFreeNodes = []GPUNodeInfo{}
	}

	overview.TaintedFreeNodes = taintedFreeNodes
	if overview.TaintedFreeNodes == nil {
		overview.TaintedFreeNodes = []GPUNodeInfo{}
	}

	overview.PartialFreeNodes = partialFreeNodes
	if overview.PartialFreeNodes == nil {
		overview.PartialFreeNodes = []GPUNodeInfo{}
	}

	// 转换并排序 namespace 统计
	var nsStats []GPUNamespaceStat
	for ns, count := range namespaceStats {
		nsStats = append(nsStats, GPUNamespaceStat{
			Namespace: ns,
			GPUCount:  count,
		})
	}
	sort.Slice(nsStats, func(i, j int) bool {
		return nsStats[i].GPUCount > nsStats[j].GPUCount
	})
	overview.NamespaceStats = nsStats
	if overview.NamespaceStats == nil {
		overview.NamespaceStats = []GPUNamespaceStat{}
	}

	// 转换并排序模型统计
	var mStats []GPUModelStat
	for model, count := range modelStats {
		mStats = append(mStats, GPUModelStat{
			ModelName: model,
			GPUCount:  count,
		})
	}
	sort.Slice(mStats, func(i, j int) bool {
		return mStats[i].GPUCount > mStats[j].GPUCount
	})
	overview.ModelStats = mStats
	if overview.ModelStats == nil {
		overview.ModelStats = []GPUModelStat{}
	}

	overview.NoModelGPUCount = noModelCount

	// 转换并排序 PD 角色统计
	var roleStats []GPUModelRoleStat
	for _, stat := range roleStatsMap {
		roleStats = append(roleStats, *stat)
	}
	sort.Slice(roleStats, func(i, j int) bool {
		return roleStats[i].ModelName < roleStats[j].ModelName
	})
	overview.ModelRoleStats = roleStats
	if overview.ModelRoleStats == nil {
		overview.ModelRoleStats = []GPUModelRoleStat{}
	}

	return overview
}
