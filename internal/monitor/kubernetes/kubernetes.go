// Package k8smon implements a Kubernetes-native monitor.
package k8smon

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

func init() {
	monitor.Register("kubernetes", New)
}

type k8sMonitor struct {
	name      string
	kind      string
	namespace string
	resName   string // resource name; for events, reused as reason filter
	check     string
	client    kubernetes.Interface
}

// New is the factory registered under the "kubernetes" type.
// It always returns an error because a Kubernetes monitor requires an injected
// client; callers must use NewWithClient instead.
func New(name string, cfg map[string]any) (monitor.Monitor, error) {
	return nil, fmt.Errorf("kubernetes monitor %q: use NewWithClient to inject a k8s client", name)
}

// NewWithClient creates a kubernetes monitor with a pre-built k8s client.
func NewWithClient(name string, cfg map[string]any, client kubernetes.Interface) (monitor.Monitor, error) {
	kind, err := cfgString(cfg, "kind", true)
	if err != nil {
		return nil, fmt.Errorf("kubernetes monitor %q: %w", name, err)
	}
	check, err := cfgString(cfg, "check", true)
	if err != nil {
		return nil, fmt.Errorf("kubernetes monitor %q: %w", name, err)
	}
	namespace, _ := cfgString(cfg, "namespace", false)
	resName, _ := cfgString(cfg, "name", false)

	return &k8sMonitor{
		name:      name,
		kind:      kind,
		namespace: namespace,
		resName:   resName,
		check:     check,
		client:    client,
	}, nil
}

// cfgString extracts a string value from a config map.
func cfgString(cfg map[string]any, key string, required bool) (string, error) {
	v, ok := cfg[key]
	if !ok {
		if required {
			return "", fmt.Errorf("missing required field %q", key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("field %q must be a string", key)
	}
	return s, nil
}

func (m *k8sMonitor) Name() string { return m.name }

func (m *k8sMonitor) Check(ctx context.Context) (state.CheckResult, error) {
	now := time.Now()
	switch m.kind {
	case "deployment":
		return m.checkDeployment(ctx, now)
	case "pod":
		return m.checkPod(ctx, now)
	case "node":
		return m.checkNode(ctx, now)
	case "event":
		return m.checkEvent(ctx, now)
	default:
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     fmt.Sprintf("unknown kind %q", m.kind),
			Timestamp:   now,
		}, nil
	}
}

// checkDeployment fetches a Deployment and evaluates the configured check.
func (m *k8sMonitor) checkDeployment(ctx context.Context, now time.Time) (state.CheckResult, error) {
	d, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, m.resName, metav1.GetOptions{})
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckError,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	var value any
	var msg string

	switch m.check {
	case "ready_replicas":
		n := d.Status.ReadyReplicas
		value = float64(n)
		msg = fmt.Sprintf("%d ready", n)
	case "available_replicas":
		n := d.Status.AvailableReplicas
		value = float64(n)
		msg = fmt.Sprintf("%d available", n)
	case "paused":
		value = d.Spec.Paused
		msg = fmt.Sprintf("paused=%v", d.Spec.Paused)
	default:
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     fmt.Sprintf("unknown check %q for deployment", m.check),
			Timestamp:   now,
		}, nil
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       value,
		Message:     msg,
		Timestamp:   now,
	}, nil
}

// checkPod fetches a Pod and evaluates the configured check.
func (m *k8sMonitor) checkPod(ctx context.Context, now time.Time) (state.CheckResult, error) {
	pod, err := m.client.CoreV1().Pods(m.namespace).Get(ctx, m.resName, metav1.GetOptions{})
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckError,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	var value any
	var msg string

	switch m.check {
	case "phase":
		value = string(pod.Status.Phase)
		msg = fmt.Sprintf("phase=%s", pod.Status.Phase)
	case "restarts":
		var total int32
		for _, cs := range pod.Status.ContainerStatuses {
			total += cs.RestartCount
		}
		value = float64(total)
		msg = fmt.Sprintf("%d restarts", total)
	case "ready_condition":
		ready := false
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		value = ready
		msg = fmt.Sprintf("ready=%v", ready)
	default:
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     fmt.Sprintf("unknown check %q for pod", m.check),
			Timestamp:   now,
		}, nil
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       value,
		Message:     msg,
		Timestamp:   now,
	}, nil
}

// checkNode fetches one or all Nodes and evaluates the configured check.
// If resName is set, a single node is fetched; otherwise all nodes are listed.
func (m *k8sMonitor) checkNode(ctx context.Context, now time.Time) (state.CheckResult, error) {
	var nodes []corev1.Node

	if m.resName != "" {
		node, err := m.client.CoreV1().Nodes().Get(ctx, m.resName, metav1.GetOptions{})
		if err != nil {
			return state.CheckResult{
				MonitorName: m.name,
				Status:      state.CheckError,
				Message:     err.Error(),
				Timestamp:   now,
			}, nil
		}
		nodes = []corev1.Node{*node}
	} else {
		list, err := m.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return state.CheckResult{
				MonitorName: m.name,
				Status:      state.CheckError,
				Message:     err.Error(),
				Timestamp:   now,
			}, nil
		}
		nodes = list.Items
	}

	var value any
	var msg string

	switch m.check {
	case "ready_condition":
		// false if any node is NOT ready
		allReady := true
		for _, node := range nodes {
			if !nodeConditionTrue(node, corev1.NodeReady) {
				allReady = false
				break
			}
		}
		value = allReady
		msg = fmt.Sprintf("all_ready=%v", allReady)

	case "disk_pressure":
		// true if any node has DiskPressure
		hasPressure := false
		for _, node := range nodes {
			if nodeConditionTrue(node, corev1.NodeDiskPressure) {
				hasPressure = true
				break
			}
		}
		value = hasPressure
		msg = fmt.Sprintf("disk_pressure=%v", hasPressure)

	case "memory_pressure":
		// true if any node has MemoryPressure
		hasPressure := false
		for _, node := range nodes {
			if nodeConditionTrue(node, corev1.NodeMemoryPressure) {
				hasPressure = true
				break
			}
		}
		value = hasPressure
		msg = fmt.Sprintf("memory_pressure=%v", hasPressure)

	default:
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     fmt.Sprintf("unknown check %q for node", m.check),
			Timestamp:   now,
		}, nil
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       value,
		Message:     msg,
		Timestamp:   now,
	}, nil
}

// nodeConditionTrue reports whether the node has the given condition set to True.
func nodeConditionTrue(node corev1.Node, condType corev1.NodeConditionType) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == condType && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// checkEvent lists Events in a namespace, optionally filtering by reason and type.
// resName is reused as a reason filter; check=="type=Warning" restricts to Warning events.
// Returns Value=true if any matching event is found.
func (m *k8sMonitor) checkEvent(ctx context.Context, now time.Time) (state.CheckResult, error) {
	list, err := m.client.CoreV1().Events(m.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckError,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	events := list.Items

	// Filter by reason when resName is provided.
	if m.resName != "" {
		var filtered []corev1.Event
		for _, ev := range events {
			if ev.Reason == m.resName {
				filtered = append(filtered, ev)
			}
		}
		events = filtered
	}

	// Filter to Warning type when check is "type=Warning".
	if m.check == "type=Warning" {
		var filtered []corev1.Event
		for _, ev := range events {
			if ev.Type == "Warning" {
				filtered = append(filtered, ev)
			}
		}
		events = filtered
	}

	found := len(events) > 0
	msg := fmt.Sprintf("found=%v (count=%d)", found, len(events))

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       found,
		Message:     msg,
		Timestamp:   now,
	}, nil
}
