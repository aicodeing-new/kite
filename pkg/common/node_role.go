package common

import corev1 "k8s.io/api/core/v1"

// IsControlPlaneNode reports whether the node carries the control-plane role.
// It checks both the modern node-role.kubernetes.io/control-plane label and the
// legacy node-role.kubernetes.io/master label (k8s < 1.20).
func IsControlPlaneNode(node *corev1.Node) bool {
	if node == nil {
		return false
	}
	labels := node.GetLabels()
	_, cp := labels["node-role.kubernetes.io/control-plane"]
	_, master := labels["node-role.kubernetes.io/master"]
	return cp || master
}
