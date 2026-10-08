package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zxh326/kite/pkg/cluster"
	"github.com/zxh326/kite/pkg/common"
	"github.com/zxh326/kite/pkg/kube"
	"github.com/zxh326/kite/pkg/model"
	promclient "github.com/zxh326/kite/pkg/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/drain"
	metricsv1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type diskStatCache struct {
	mu         sync.RWMutex
	data       map[string]diskStat // keyed by node name
	fetchedAt  time.Time
	refreshing bool
}

const diskStatsTTL = 60 * time.Second

// global per-cluster cache so concurrent requests share one fetch
var (
	diskStatsCacheMu sync.Mutex
	diskStatsCaches  = map[*cluster.ClientSet]*diskStatCache{} // keyed by concrete cluster client
)

func getDiskStatCache(cs *cluster.ClientSet) *diskStatCache {
	diskStatsCacheMu.Lock()
	defer diskStatsCacheMu.Unlock()
	if c, ok := diskStatsCaches[cs]; ok {
		return c
	}
	c := &diskStatCache{data: map[string]diskStat{}}
	diskStatsCaches[cs] = c
	return c
}

type NodeHandler struct {
	*GenericResourceHandler[*corev1.Node, *corev1.NodeList]
}

func NewNodeHandler() *NodeHandler {
	return &NodeHandler{
		GenericResourceHandler: NewGenericResourceHandler[*corev1.Node, *corev1.NodeList](common.Nodes),
	}
}

// Node lists include metrics, pod counts, roles, and disk information assembled
// outside the Kubernetes Node object. Keep polling until watch events can be
// enriched with the same data.
func (h *NodeHandler) WatchSupported() bool {
	return false
}

// recordNodeAudit logs a node operation (drain/cordon/uncordon/taint/untaint) to the audit history.
func (h *NodeHandler) recordNodeAudit(c *gin.Context, nodeName, opType string, success bool, errMsg string) {
	cs, ok := c.MustGet("cluster").(*cluster.ClientSet)
	if !ok {
		return
	}
	user, ok := c.MustGet("user").(model.User)
	if !ok {
		return
	}
	history := model.ResourceHistory{
		ClusterName:     cs.Name,
		ResourceType:    string(common.Nodes),
		ResourceName:    nodeName,
		Namespace:       "",
		OperationType:   opType,
		OperationSource: "manual",
		Success:         success,
		ErrorMessage:    errMsg,
		OperatorID:      user.ID,
	}
	if err := model.DB.Create(&history).Error; err != nil {
		klog.Errorf("Failed to create node audit history: %v", err)
	}
}

// DrainNode drains a node by evicting all pods
func (h *NodeHandler) DrainNode(c *gin.Context) {
	nodeName := c.Param("name")
	ctx := c.Request.Context()
	cs := c.MustGet("cluster").(*cluster.ClientSet)
	// Parse the request body for drain options
	var drainRequest struct {
		Force            bool `json:"force" binding:"required"`
		GracePeriod      int  `json:"gracePeriod" binding:"min=0"`
		DeleteLocal      bool `json:"deleteLocalData"`
		IgnoreDaemonsets bool `json:"ignoreDaemonsets"`
	}

	if err := c.ShouldBindJSON(&drainRequest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Get the node first to ensure it exists
	var node corev1.Node
	if err := cs.K8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		if errors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	drainer := &drain.Helper{
		Ctx:                 ctx,
		Client:              cs.K8sClient.ClientSet,
		Force:               drainRequest.Force,
		GracePeriodSeconds:  drainRequest.GracePeriod,
		IgnoreAllDaemonSets: drainRequest.IgnoreDaemonsets,
		DeleteEmptyDirData:  drainRequest.DeleteLocal,
		Out:                 io.Discard,
		ErrOut:              io.Discard,
	}

	if err := drain.RunCordonOrUncordon(drainer, &node, false); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cordon node: " + err.Error()})
		return
	}

	podDeleteList, errs := drainer.GetPodsForDeletion(nodeName)
	if len(errs) > 0 {
		errMsg := ""
		for i, item := range errs {
			if i > 0 {
				errMsg += "; "
			}
			errMsg += item.Error()
		}
		c.JSON(http.StatusConflict, gin.H{"error": errMsg})
		return
	}

	if err := drainer.DeleteOrEvictPods(podDeleteList.Pods()); err != nil {
		h.recordNodeAudit(c, nodeName, "drain", false, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to drain node: " + err.Error()})
		return
	}

	h.recordNodeAudit(c, nodeName, "drain", true, "")
	c.JSON(http.StatusOK, gin.H{
		"message":  fmt.Sprintf("Node %s drained successfully", nodeName),
		"node":     node.Name,
		"pods":     len(podDeleteList.Pods()),
		"warnings": podDeleteList.Warnings(),
	})
}

func (h *NodeHandler) markNodeSchedulable(ctx context.Context, client *kube.K8sClient, nodeName string, schedulable bool) error {
	// Get the current node
	var node corev1.Node
	if err := client.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		return err
	}
	node.Spec.Unschedulable = !schedulable
	if err := client.Update(ctx, &node); err != nil {
		return err
	}
	return nil
}

// CordonNode marks a node as unschedulable
func (h *NodeHandler) CordonNode(c *gin.Context) {
	nodeName := c.Param("name")
	ctx := c.Request.Context()
	cs := c.MustGet("cluster").(*cluster.ClientSet)

	if err := h.markNodeSchedulable(ctx, cs.K8sClient, nodeName, false); err != nil {
		h.recordNodeAudit(c, nodeName, "cordon", false, err.Error())
		if errors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
			return
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	h.recordNodeAudit(c, nodeName, "cordon", true, "")
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Node %s cordoned successfully", nodeName),
	})
}

// UncordonNode marks a node as schedulable
func (h *NodeHandler) UncordonNode(c *gin.Context) {
	nodeName := c.Param("name")
	ctx := c.Request.Context()
	cs := c.MustGet("cluster").(*cluster.ClientSet)

	if err := h.markNodeSchedulable(ctx, cs.K8sClient, nodeName, true); err != nil {
		h.recordNodeAudit(c, nodeName, "uncordon", false, err.Error())
		if errors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
			return
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	h.recordNodeAudit(c, nodeName, "uncordon", true, "")
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Node %s uncordoned successfully", nodeName),
	})
}

// RestoreNodeScheduling makes a node schedulable again in a single patch:
// it removes every taint and clears spec.unschedulable.
func (h *NodeHandler) RestoreNodeScheduling(c *gin.Context) {
	nodeName := c.Param("name")
	ctx := c.Request.Context()
	cs := c.MustGet("cluster").(*cluster.ClientSet)

	var node corev1.Node
	if err := cs.K8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		h.recordNodeAudit(c, nodeName, "restore", false, err.Error())
		if errors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	removedTaints := len(node.Spec.Taints)
	wasUnschedulable := node.Spec.Unschedulable
	// Explicitly send both fields even when the cached node already looks clean.
	// Avoid a resourceVersion precondition so unrelated concurrent node updates
	// do not cause conflicts, and leave every other field untouched.
	patch := client.RawPatch(types.MergePatchType, []byte(`{"spec":{"taints":null,"unschedulable":false}}`))
	if err := cs.K8sClient.Patch(ctx, &node, patch); err != nil {
		h.recordNodeAudit(c, nodeName, "restore", false, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restore node scheduling: " + err.Error()})
		return
	}

	h.recordNodeAudit(c, nodeName, "restore", true, "")
	c.JSON(http.StatusOK, gin.H{
		"message":       fmt.Sprintf("Node %s restored for scheduling: %d taint(s) removed", node.Name, removedTaints),
		"node":          node.Name,
		"removedTaints": removedTaints,
		"uncordoned":    wasUnschedulable,
	})
}

// TaintNode adds or updates taints on a node
func (h *NodeHandler) TaintNode(c *gin.Context) {
	nodeName := c.Param("name")
	ctx := c.Request.Context()
	cs := c.MustGet("cluster").(*cluster.ClientSet)

	// Parse the request body for taint information
	var taintRequest struct {
		Key    string `json:"key" binding:"required"`
		Value  string `json:"value"`
		Effect string `json:"effect" binding:"required,oneof=NoSchedule PreferNoSchedule NoExecute"`
	}

	if err := c.ShouldBindJSON(&taintRequest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Get the current node
	var node corev1.Node
	if err := cs.K8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		if errors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Create the new taint
	newTaint := corev1.Taint{
		Key:    taintRequest.Key,
		Value:  taintRequest.Value,
		Effect: corev1.TaintEffect(taintRequest.Effect),
	}

	// Check if taint with same key already exists and update it, otherwise add new taint
	found := false
	for i, taint := range node.Spec.Taints {
		if taint.Key == taintRequest.Key {
			node.Spec.Taints[i] = newTaint
			found = true
			break
		}
	}

	if !found {
		node.Spec.Taints = append(node.Spec.Taints, newTaint)
	}

	// Update the node
	if err := cs.K8sClient.Update(ctx, &node); err != nil {
		h.recordNodeAudit(c, nodeName, "taint", false, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to taint node: " + err.Error()})
		return
	}

	h.recordNodeAudit(c, nodeName, "taint", true, "")
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Node %s tainted successfully", nodeName),
		"node":    node.Name,
		"taint":   newTaint,
	})
}

// UntaintNode removes a taint from a node
func (h *NodeHandler) UntaintNode(c *gin.Context) {
	nodeName := c.Param("name")
	ctx := c.Request.Context()
	cs := c.MustGet("cluster").(*cluster.ClientSet)

	// Parse the request body for taint key to remove
	var untaintRequest struct {
		Key string `json:"key" binding:"required"`
	}

	if err := c.ShouldBindJSON(&untaintRequest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Get the current node
	var node corev1.Node
	if err := cs.K8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		if errors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Find and remove the taint with the specified key
	originalLength := len(node.Spec.Taints)
	var newTaints []corev1.Taint
	for _, taint := range node.Spec.Taints {
		if taint.Key != untaintRequest.Key {
			newTaints = append(newTaints, taint)
		}
	}
	node.Spec.Taints = newTaints

	if len(newTaints) == originalLength {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Taint with key '%s' not found on node", untaintRequest.Key)})
		return
	}

	// Update the node
	if err := cs.K8sClient.Update(ctx, &node); err != nil {
		h.recordNodeAudit(c, nodeName, "untaint", false, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to untaint node: " + err.Error()})
		return
	}

	h.recordNodeAudit(c, nodeName, "untaint", true, "")
	c.JSON(http.StatusOK, gin.H{
		"message":         fmt.Sprintf("Taint with key '%s' removed from node %s successfully", untaintRequest.Key, nodeName),
		"node":            node.Name,
		"removedTaintKey": untaintRequest.Key,
	})
}

func (h *NodeHandler) List(c *gin.Context) {
	cs := c.MustGet("cluster").(*cluster.ClientSet)
	ctx := c.Request.Context()

	var nodes corev1.NodeList
	if err := cs.K8sClient.List(ctx, &nodes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list nodes: " + err.Error()})
		return
	}

	// --- CPU/Memory: Prometheus (node-exporter) preferred over metrics-server ---
	// Same queries as Lens/OpenLens: node_cpu_seconds_total + node_memory_MemAvailable_bytes.
	var promMetrics map[string]*promclient.NodeInstantMetric
	if cs.PromClient != nil {
		nodeIPToName := buildNodeIPToNameMap(nodes.Items)
		m, err := cs.PromClient.GetNodeInstantMetrics(ctx, nodeIPToName)
		if err != nil {
			klog.V(4).Infof("Prometheus node metrics unavailable for cluster %s, falling back to metrics-server: %v", cs.Name, err)
		} else {
			promMetrics = m
		}
	}

	// Fallback: metrics-server (used only when Prometheus not available)
	var nodeMetricsItems []metricsv1.NodeMetrics
	if promMetrics == nil && cs.K8sClient.MetricsClient != nil {
		metricsList, err := cs.K8sClient.MetricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
		if err != nil {
			klog.Warningf("Failed to list node metrics: %v", err)
		} else {
			nodeMetricsItems = metricsList.Items
		}
	}

	nodeMetricsMap := buildNodeMetricsMap(nodeMetricsItems)
	nodeResourceRequests := listNodeResourceRequests(ctx, cs.K8sClient, nodes.Items)

	result := &common.NodeListWithMetrics{
		TypeMeta: nodes.TypeMeta,
		ListMeta: nodes.ListMeta,
		Items:    make([]*common.NodeWithMetrics, len(nodes.Items)),
	}
	for i, node := range nodes.Items {
		metricsCell := &common.MetricsCell{}
		metricsCell.CPULimit = node.Status.Allocatable.Cpu().MilliValue()
		metricsCell.MemoryLimit = node.Status.Allocatable.Memory().Value()
		metricsCell.PodsLimit = node.Status.Allocatable.Pods().Value()

		// Prefer Prometheus, fall back to metrics-server
		if pm, ok := promMetrics[node.Name]; ok {
			metricsCell.CPUUsage = pm.CPUUsageMillicores
			metricsCell.MemoryUsage = pm.MemoryUsageBytes
		} else if nm, ok := nodeMetricsMap[node.Name]; ok {
			if cpuQuantity, ok := nm.Usage["cpu"]; ok {
				metricsCell.CPUUsage = cpuQuantity.MilliValue()
			}
			if memQuantity, ok := nm.Usage["memory"]; ok {
				metricsCell.MemoryUsage = memQuantity.Value()
			}
		}

		if requests, exists := nodeResourceRequests[node.Name]; exists {
			metricsCell.CPURequest = requests.CPURequest
			metricsCell.MemoryRequest = requests.MemoryRequest
			metricsCell.Pods = requests.Pods
		}
		result.Items[i] = &common.NodeWithMetrics{
			Node:    &node,
			Metrics: metricsCell,
		}
	}
	sort.Slice(result.Items, func(i, j int) bool {
		return result.Items[i].Name < result.Items[j].Name
	})
	c.JSON(http.StatusOK, result)
}

func (h *NodeHandler) registerCustomRoutes(group *gin.RouterGroup) {
	group.GET("/_all/disk-stats", h.DiskStats)
	group.POST("/_all/:name/drain", h.DrainNode)
	group.POST("/_all/:name/cordon", h.CordonNode)
	group.POST("/_all/:name/uncordon", h.UncordonNode)
	group.POST("/_all/:name/taint", h.TaintNode)
	group.POST("/_all/:name/untaint", h.UntaintNode)
	group.POST("/_all/:name/restore", h.RestoreNodeScheduling)
}

type diskStat struct {
	DiskUsage    int64 `json:"diskUsage"`
	DiskCapacity int64 `json:"diskCapacity"`
}

// DiskStats returns disk usage for all nodes.
// It queries Prometheus (instant query, sub-second) when available, and falls back
// to a background-cached kubelet stats/summary otherwise.
func (h *NodeHandler) DiskStats(c *gin.Context) {
	cs := c.MustGet("cluster").(*cluster.ClientSet)
	ctx := c.Request.Context()

	// --- Fast path: Prometheus instant query ---
	if cs.PromClient != nil {
		var nodes corev1.NodeList
		if err := cs.K8sClient.List(ctx, &nodes); err == nil {
			nodeIPToName := buildNodeIPToNameMap(nodes.Items)
			metrics, err := cs.PromClient.GetNodeDiskMetrics(ctx, nodeIPToName)
			if err == nil && len(metrics) > 0 {
				result := make(map[string]diskStat, len(metrics))
				for nodeName, m := range metrics {
					result[nodeName] = diskStat{DiskUsage: m.DiskUsed, DiskCapacity: m.DiskTotal}
				}
				c.JSON(http.StatusOK, result)
				return
			}
			klog.V(4).Infof("Prometheus disk metrics unavailable for cluster %s, using kubelet fallback: %v", cs.Name, err)
		}
	}

	// --- Slow path: background-cached kubelet stats/summary ---
	cache := getDiskStatCache(cs)

	cache.mu.RLock()
	cachedData := make(map[string]diskStat, len(cache.data))
	for k, v := range cache.data {
		cachedData[k] = v
	}
	age := time.Since(cache.fetchedAt)
	refreshing := cache.refreshing
	cache.mu.RUnlock()

	// Trigger background refresh if cache is stale or empty
	if (age > diskStatsTTL || len(cachedData) == 0) && !refreshing {
		cache.mu.Lock()
		if !cache.refreshing {
			cache.refreshing = true
			go func() {
				defer func() {
					cache.mu.Lock()
					cache.refreshing = false
					cache.mu.Unlock()
				}()
				bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()

				var nodes corev1.NodeList
				if err := cs.K8sClient.List(bgCtx, &nodes); err != nil {
					klog.Warningf("disk-stats refresh: failed to list nodes for cluster %s: %v", cs.Name, err)
					return
				}
				nodeNames := make([]string, len(nodes.Items))
				for i, node := range nodes.Items {
					nodeNames[i] = node.Name
				}

				raw := fetchNodeDiskStats(bgCtx, cs, nodeNames)
				newData := make(map[string]diskStat, len(raw))
				for k, v := range raw {
					newData[k] = diskStat{DiskUsage: v[0], DiskCapacity: v[1]}
				}

				cache.mu.Lock()
				cache.data = newData
				cache.fetchedAt = time.Now()
				cache.mu.Unlock()
				klog.V(4).Infof("disk-stats cache refreshed for cluster %s: %d nodes", cs.Name, len(newData))
			}()
		}
		cache.mu.Unlock()
	}

	// If cache is empty, wait briefly for the first refresh to complete
	if len(cachedData) == 0 {
		for i := 0; i < 120; i++ {
			time.Sleep(500 * time.Millisecond)
			cache.mu.RLock()
			if len(cache.data) > 0 || !cache.refreshing {
				for k, v := range cache.data {
					cachedData[k] = v
				}
				cache.mu.RUnlock()
				break
			}
			cache.mu.RUnlock()
		}
	}

	c.JSON(http.StatusOK, cachedData)
}

// buildNodeIPToNameMap builds a map of InternalIP → nodeName from the node list.
// Used to resolve node-exporter "instance" labels (ip:port) to node names.
func buildNodeIPToNameMap(nodes []corev1.Node) map[string]string {
	m := make(map[string]string, len(nodes))
	for _, node := range nodes {
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				m[addr.Address] = node.Name
			}
		}
	}
	return m
}

// nodeStatsSummary is a minimal representation of the kubelet /stats/summary response.
type nodeStatsSummary struct {
	Node struct {
		Fs *struct {
			UsedBytes     *uint64 `json:"usedBytes"`
			CapacityBytes *uint64 `json:"capacityBytes"`
		} `json:"fs"`
	} `json:"node"`
}

// fetchNodeDiskStats concurrently fetches kubelet stats/summary for each node.
func fetchNodeDiskStats(ctx context.Context, cs *cluster.ClientSet, nodeNames []string) map[string][2]int64 {
	result := make(map[string][2]int64, len(nodeNames))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, name := range nodeNames {
		wg.Add(1)
		go func(nodeName string) {
			defer wg.Done()
			data, err := cs.K8sClient.ClientSet.CoreV1().RESTClient().
				Get().
				Resource("nodes").
				Name(nodeName).
				SubResource("proxy").
				Suffix("stats/summary").
				DoRaw(ctx)
			if err != nil {
				klog.V(4).Infof("Failed to get stats/summary for node %s: %v", nodeName, err)
				return
			}
			var summary nodeStatsSummary
			if err := json.Unmarshal(data, &summary); err != nil {
				klog.V(4).Infof("Failed to unmarshal stats/summary for node %s: %v", nodeName, err)
				return
			}
			if summary.Node.Fs != nil &&
				summary.Node.Fs.UsedBytes != nil &&
				summary.Node.Fs.CapacityBytes != nil {
				mu.Lock()
				result[nodeName] = [2]int64{
					int64(*summary.Node.Fs.UsedBytes),
					int64(*summary.Node.Fs.CapacityBytes),
				}
				mu.Unlock()
			}
		}(name)
	}
	wg.Wait()
	return result
}

func buildNodeMetricsMap(nodeMetrics []metricsv1.NodeMetrics) map[string]metricsv1.NodeMetrics {
	metricsMap := make(map[string]metricsv1.NodeMetrics, len(nodeMetrics))
	for _, nodeMetric := range nodeMetrics {
		metricsMap[nodeMetric.Name] = nodeMetric
	}
	return metricsMap
}

func listNodeResourceRequests(ctx context.Context, k8sClient *kube.K8sClient, nodes []corev1.Node) map[string]common.MetricsCell {
	if !k8sClient.CacheEnabled {
		return listNodeResourceRequestsFromAllPods(ctx, k8sClient)
	}

	nodeResourceRequests := make(map[string]common.MetricsCell, len(nodes))
	for _, node := range nodes {
		var nodePods corev1.PodList
		if err := k8sClient.List(ctx, &nodePods, client.MatchingFields{"spec.nodeName": node.Name}); err != nil {
			klog.Warningf("Failed to list pods for node %s: %v", node.Name, err)
			continue
		}

		var metrics common.MetricsCell
		for i := range nodePods.Items {
			addPodResources(&metrics, &nodePods.Items[i])
		}
		nodeResourceRequests[node.Name] = metrics
	}
	return nodeResourceRequests
}

func listNodeResourceRequestsFromAllPods(ctx context.Context, k8sClient *kube.K8sClient) map[string]common.MetricsCell {
	var allPods corev1.PodList
	if err := k8sClient.List(ctx, &allPods); err != nil {
		klog.Warningf("Failed to list pods: %v", err)
		return map[string]common.MetricsCell{}
	}

	nodeResourceRequests := make(map[string]common.MetricsCell)
	for i := range allPods.Items {
		pod := &allPods.Items[i]
		if pod.Spec.NodeName == "" {
			continue
		}

		metrics := nodeResourceRequests[pod.Spec.NodeName]
		addPodResources(&metrics, pod)
		nodeResourceRequests[pod.Spec.NodeName] = metrics
	}
	return nodeResourceRequests
}

func addPodResources(metrics *common.MetricsCell, pod *corev1.Pod) {
	metrics.Pods++
	for _, container := range pod.Spec.Containers {
		if cpuRequest := container.Resources.Requests.Cpu(); cpuRequest != nil {
			metrics.CPURequest += cpuRequest.MilliValue()
		}
		if memoryRequest := container.Resources.Requests.Memory(); memoryRequest != nil {
			metrics.MemoryRequest += memoryRequest.Value()
		}
	}
}
