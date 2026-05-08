// Package k8smon implements a Kubernetes-native monitor.
// This is a stub for Task 12 — the full implementation is in Task 15.
package k8smon

import (
	"fmt"

	"github.com/mfeldheim/klyra/internal/monitor"
	"k8s.io/client-go/kubernetes"
)

// NewWithClient creates a kubernetes monitor with a pre-built k8s client.
// This stub returns an error; it will be replaced by the real implementation in Task 15.
func NewWithClient(name string, cfg map[string]any, client kubernetes.Interface) (monitor.Monitor, error) {
	return nil, fmt.Errorf("kubernetes monitor not yet implemented")
}
