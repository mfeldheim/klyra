// Package k8smon implements a Kubernetes-native monitor.
// This is a stub for Task 12 — the full implementation is in Task 15.
package k8smon

import (
	"github.com/mfeldheim/klyra/internal/monitor"
	"k8s.io/client-go/kubernetes"
)

// NewWithClient creates a kubernetes monitor with a pre-built k8s client.
// This stub panics; it will be replaced by the real implementation in Task 15.
func NewWithClient(name string, cfg map[string]any, client kubernetes.Interface) (monitor.Monitor, error) {
	panic("kubernetes monitor not yet implemented")
}
