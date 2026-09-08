package handlers

import (
	"context"
	"testing"

	"github.com/zxh326/kite/pkg/cluster"
	"github.com/zxh326/kite/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGPUResourceMatcher(t *testing.T) {
	tests := []struct {
		name         string
		rules        []string
		resourceName string
		want         bool
	}{
		{name: "standard vendor resource", resourceName: "future-vendor.example/gpu", want: true},
		{name: "virtual GPU", resourceName: "vendor.example/vgpu", want: true},
		{name: "legacy vendor prefixed GPU", resourceName: "alpha.kubernetes.io/nvidia-gpu", want: true},
		{name: "Intel GPU domain", resourceName: "gpu.intel.com/i915", want: true},
		{name: "NVIDIA MIG profile", resourceName: "nvidia.com/mig-1g.5gb", want: true},
		{name: "GPU share", resourceName: "vendor.example/gpu.shared", want: true},
		{name: "unrelated extended resource", resourceName: "vendor.example/rdma", want: false},
		{name: "GPU memory is not a device count", resourceName: "vendor.example/gpu-memory", want: false},
		{name: "configured non-standard accelerator", rules: []string{"huawei.com/Ascend910"}, resourceName: "huawei.com/Ascend910", want: true},
		{name: "configured exclusion wins", rules: []string{"!future-vendor.example/gpu"}, resourceName: "future-vendor.example/gpu", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newGPUResourceMatcher(tt.rules).matches(tt.resourceName); got != tt.want {
				t.Fatalf("matches(%q) = %t, want %t", tt.resourceName, got, tt.want)
			}
		})
	}
}

func TestGPUDiscoveryAndPodUsageUseSameMatcher(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	resourceName := corev1.ResourceName("future-vendor.example/gpu")
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu-node"},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				resourceName: resource.MustParse("8"),
				"cpu":        resource.MustParse("64"),
			},
			Allocatable: corev1.ResourceList{resourceName: resource.MustParse("7")},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "default"},
		Spec: corev1.PodSpec{
			NodeName: "gpu-node",
			Containers: []corev1.Container{
				{
					Name: "request-container",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{resourceName: resource.MustParse("2")},
						Limits:   corev1.ResourceList{resourceName: resource.MustParse("2")},
					},
				},
				{
					Name: "limit-only-container",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{resourceName: resource.MustParse("1")},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	clientSet := &cluster.ClientSet{
		K8sClient: &kube.K8sClient{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(node, pod).Build(),
		},
	}
	matcher := newGPUResourceMatcher(nil)

	nodes, err := getGPUNodes(context.Background(), clientSet, matcher)
	if err != nil {
		t.Fatalf("getGPUNodes() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].Capacity != 8 || nodes[0].Allocatable != 7 || nodes[0].GPUType != string(resourceName) {
		t.Fatalf("unexpected GPU nodes: %#v", nodes)
	}

	usage, err := getGPUUsageFromPods(context.Background(), clientSet, matcher)
	if err != nil {
		t.Fatalf("getGPUUsageFromPods() error = %v", err)
	}
	if usage["gpu-node"] != 3 {
		t.Fatalf("GPU usage = %d, want 3", usage["gpu-node"])
	}
}

func TestExtractGPUFromContainersChecksLimitsPerContainer(t *testing.T) {
	obj := map[string]interface{}{
		"containers": []interface{}{
			map[string]interface{}{
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{"vendor.example/gpu": "1"},
				},
			},
			map[string]interface{}{
				"resources": map[string]interface{}{
					"limits": map[string]interface{}{"vendor.example/gpu": "2"},
				},
			},
		},
	}

	if got := extractGPUFromContainers(obj, []string{"containers"}, newGPUResourceMatcher(nil)); got != 3 {
		t.Fatalf("extractGPUFromContainers() = %d, want 3", got)
	}
}
