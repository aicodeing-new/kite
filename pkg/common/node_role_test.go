package common

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsControlPlaneNode(t *testing.T) {
	tests := []struct {
		name  string
		node  *corev1.Node
		want  bool
	}{
		{
			name:  "nil node",
			node:  nil,
			want:  false,
		},
		{
			name:  "nil labels",
			node:  &corev1.Node{},
			want:  false,
		},
		{
			name:  "control-plane label",
			node:  &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""}}},
			want:  true,
		},
		{
			name:  "legacy master label",
			node:  &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"node-role.kubernetes.io/master": ""}}},
			want:  true,
		},
		{
			name:  "both control-plane and master labels",
			node:  &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"node-role.kubernetes.io/control-plane": "", "node-role.kubernetes.io/master": ""}}},
			want:  true,
		},
		{
			name:  "worker label only",
			node:  &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"node-role.kubernetes.io/worker": ""}}},
			want:  false,
		},
		{
			name:  "no node-role labels",
			node:  &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"kubernetes.io/os": "linux"}}},
			want:  false,
		},
		{
			name:  "etcd label only",
			node:  &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"node-role.kubernetes.io/etcd": ""}}},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsControlPlaneNode(tt.node); got != tt.want {
				t.Errorf("IsControlPlaneNode() = %v, want %v", got, tt.want)
			}
		})
	}
}
