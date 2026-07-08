// Package k8smon implements a Kubernetes-native monitor.
package k8smon

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	window    time.Duration // for events: only consider events within this window
	exemptSingleReplicaDeploymentsInZeroReady bool
	client    kubernetes.Interface
}

type workloadReadiness struct {
	kind      string
	namespace string
	name      string
	ready     int32
	desired   int32
	selector  string
}

func (w workloadReadiness) message() string {
	return fmt.Sprintf("%s/%s/%s (%d/%d)", w.kind, w.namespace, w.name, w.ready, w.desired)
}

func (w workloadReadiness) messageWithNodes(nodes string) string {
	if nodes == "" {
		return w.message()
	}
	return fmt.Sprintf("%s %s", w.message(), nodes)
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
	check, _ := cfgString(cfg, "check", false)
	namespace, _ := cfgString(cfg, "namespace", false)
	resName, _ := cfgString(cfg, "name", false)
	exemptSingleReplicaDeploymentsInZeroReady, err := cfgBool(cfg, "exempt_single_replica_deployments", false)
	if err != nil {
		return nil, fmt.Errorf("kubernetes monitor %q: %w", name, err)
	}
	if !hasCfgKey(cfg, "exempt_single_replica_deployments") {
		exemptSingleReplicaDeploymentsInZeroReady = true
	}

	var window time.Duration
	if v, ok := cfg["window"]; ok {
		if s, ok := v.(string); ok && s != "" {
			d, err := time.ParseDuration(s)
			if err != nil {
				return nil, fmt.Errorf("kubernetes monitor %q: invalid 'window': %w", name, err)
			}
			window = d
		}
	}

	switch kind {
	case "deployment", "pod", "node":
		if check == "" {
			return nil, fmt.Errorf("kubernetes monitor %q: kind %q requires a 'check' field", name, kind)
		}
	}

	return &k8sMonitor{
		name:      name,
		kind:      kind,
		namespace: namespace,
		resName:   resName,
		check:     check,
		window:    window,
		exemptSingleReplicaDeploymentsInZeroReady: exemptSingleReplicaDeploymentsInZeroReady,
		client:    client,
	}, nil
}

func hasCfgKey(cfg map[string]any, key string) bool {
	_, ok := cfg[key]
	return ok
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

func cfgBool(cfg map[string]any, key string, required bool) (bool, error) {
	v, ok := cfg[key]
	if !ok {
		if required {
			return false, fmt.Errorf("missing required field %q", key)
		}
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("field %q must be a boolean", key)
	}
	return b, nil
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
	case "pods_ready":
		return m.checkPodsReady(ctx, now)
	case "workloads_ready":
		return m.checkWorkloadsReady(ctx, now)
	case "workloads_zero_ready":
		return m.checkWorkloadsZeroReady(ctx, now)
	case "workloads_partially_ready":
		return m.checkWorkloadsPartiallyReady(ctx, now)
	default:
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     fmt.Sprintf("unknown kind %q", m.kind),
			Timestamp:   now,
		}, nil
	}
}

func (m *k8sMonitor) listWorkloadReadiness(ctx context.Context, listOpts metav1.ListOptions) ([]workloadReadiness, error) {
	var workloads []workloadReadiness

	deps, err := m.client.AppsV1().Deployments(m.namespace).List(ctx, listOpts)
	if err != nil {
		return nil, err
	}
	for _, d := range deps.Items {
		if d.Spec.Paused {
			continue
		}
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		selector := ""
		if d.Spec.Selector != nil {
			selector = metav1.FormatLabelSelector(d.Spec.Selector)
			if selector == "<none>" {
				selector = ""
			}
		}
		workloads = append(workloads, workloadReadiness{
			kind:      "deploy",
			namespace: d.Namespace,
			name:      d.Name,
			ready:     d.Status.ReadyReplicas,
			desired:   desired,
			selector:  selector,
		})
	}

	stss, err := m.client.AppsV1().StatefulSets(m.namespace).List(ctx, listOpts)
	if err != nil {
		return nil, err
	}
	for _, s := range stss.Items {
		desired := int32(1)
		if s.Spec.Replicas != nil {
			desired = *s.Spec.Replicas
		}
		selector := ""
		if s.Spec.Selector != nil {
			selector = metav1.FormatLabelSelector(s.Spec.Selector)
			if selector == "<none>" {
				selector = ""
			}
		}
		workloads = append(workloads, workloadReadiness{
			kind:      "sts",
			namespace: s.Namespace,
			name:      s.Name,
			ready:     s.Status.ReadyReplicas,
			desired:   desired,
			selector:  selector,
		})
	}

	dss, err := m.client.AppsV1().DaemonSets(m.namespace).List(ctx, listOpts)
	if err != nil {
		return nil, err
	}
	for _, ds := range dss.Items {
		selector := ""
		if ds.Spec.Selector != nil {
			selector = metav1.FormatLabelSelector(ds.Spec.Selector)
			if selector == "<none>" {
				selector = ""
			}
		}
		workloads = append(workloads, workloadReadiness{
			kind:      "ds",
			namespace: ds.Namespace,
			name:      ds.Name,
			ready:     ds.Status.NumberReady,
			desired:   ds.Status.DesiredNumberScheduled,
			selector:  selector,
		})
	}

	return workloads, nil
}

func (m *k8sMonitor) workloadNodeSummary(ctx context.Context, w workloadReadiness, nodeStatus map[string]string) string {
	if w.selector == "" || w.selector == "<none>" {
		return "nodes=unknown"
	}

	pods, err := m.client.CoreV1().Pods(w.namespace).List(ctx, metav1.ListOptions{LabelSelector: w.selector})
	if err != nil {
		return "nodes=unknown"
	}

	nodes := map[string]struct{}{}
	pending := false
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if pod.Spec.NodeName == "" {
			pending = true
			continue
		}
		nodes[pod.Spec.NodeName] = struct{}{}
	}

	if len(nodes) == 0 {
		if pending {
			return "nodes=pending:Unscheduled"
		}
		return "nodes=none"
	}

	names := make([]string, 0, len(nodes))
	for n := range nodes {
		names = append(names, n)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names)+1)
	for _, name := range names {
		status, ok := nodeStatus[name]
		if !ok {
			node, err := m.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				status = "Unknown"
			} else if nodeConditionTrue(*node, corev1.NodeReady) {
				status = "Ready"
			} else {
				status = "NotReady"
			}
			nodeStatus[name] = status
		}
		parts = append(parts, fmt.Sprintf("%s:%s", name, status))
	}
	if pending {
		parts = append(parts, "pending:Unscheduled")
	}

	return fmt.Sprintf("nodes=%s", strings.Join(parts, ","))
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
	listOpts := metav1.ListOptions{}
	if m.resName != "" {
		listOpts.FieldSelector = "reason=" + m.resName
	}

	list, err := m.client.CoreV1().Events(m.namespace).List(ctx, listOpts)
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckError,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	events := list.Items

	// Filter by recency when a window is configured.
	if m.window > 0 {
		cutoff := now.Add(-m.window)
		var recent []corev1.Event
		for _, ev := range events {
			ts := ev.LastTimestamp.Time
			if ts.IsZero() && ev.EventTime.Time != (time.Time{}) {
				ts = ev.EventTime.Time
			}
			if ts.After(cutoff) {
				recent = append(recent, ev)
			}
		}
		events = recent
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
	} else if m.check != "" {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     fmt.Sprintf("unknown check %q for event", m.check),
			Timestamp:   now,
		}, nil
	}

	found := len(events) > 0
	var msg string
	if found {
		names := make([]string, 0, len(events))
		seen := make(map[string]bool)
		for _, ev := range events {
			n := ev.InvolvedObject.Name
			if n != "" && !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			msg = fmt.Sprintf("%s (count=%d, pods: %s)", m.resName, len(events), strings.Join(names, ", "))
		} else {
			msg = fmt.Sprintf("%s (count=%d)", m.resName, len(events))
		}
	} else {
		msg = fmt.Sprintf("%s: none in window", m.resName)
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       found,
		Message:     msg,
		Timestamp:   now,
	}, nil
}

// checkPodsReady lists pods in the configured namespace, skipping completed and
// terminating pods, and returns Value=true if any active pod is not Ready.
// The check field is used as an optional label selector (e.g. "app=myapp").
// When no active pods are found (empty namespace or all filtered out), returns
// Value=false — callers should be aware that zero pods is indistinguishable
// from all-pods-ready without additional monitoring.
func (m *k8sMonitor) checkPodsReady(ctx context.Context, now time.Time) (state.CheckResult, error) {
	listOpts := metav1.ListOptions{}
	if m.check != "" {
		listOpts.LabelSelector = m.check
	}

	list, err := m.client.CoreV1().Pods(m.namespace).List(ctx, listOpts)
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckError,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	var notReady []string
	for _, pod := range list.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if pod.DeletionTimestamp != nil {
			continue
		}
		ready := false
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready {
			notReady = append(notReady, pod.Name)
		}
	}

	anyNotReady := len(notReady) > 0
	var msg string
	if anyNotReady {
		msg = fmt.Sprintf("not ready: %s", strings.Join(notReady, ", "))
	} else {
		msg = "all pods ready"
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       anyNotReady,
		Message:     msg,
		Timestamp:   now,
	}, nil
}

// checkWorkloadsReady lists Deployments, StatefulSets, and DaemonSets in the configured
// namespace, and returns Value=true if any workload has fewer ready replicas than desired.
// Paused Deployments are skipped. The check field is used as an optional label selector.
// When no namespace is set (empty string), checks cluster-wide.
func (m *k8sMonitor) checkWorkloadsReady(ctx context.Context, now time.Time) (state.CheckResult, error) {
	listOpts := metav1.ListOptions{}
	if m.check != "" {
		listOpts.LabelSelector = m.check
	}

	workloads, err := m.listWorkloadReadiness(ctx, listOpts)
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckError,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	var notReady []string
	nodeStatus := map[string]string{}
	for _, w := range workloads {
		if w.desired > 0 && w.ready < w.desired {
			notReady = append(notReady, w.messageWithNodes(m.workloadNodeSummary(ctx, w, nodeStatus)))
		}
	}

	anyNotReady := len(notReady) > 0
	var msg string
	if anyNotReady {
		msg = fmt.Sprintf("not ready: %s", strings.Join(notReady, ", "))
	} else {
		msg = "all workloads ready"
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       !anyNotReady,
		Message:     msg,
		Timestamp:   now,
	}, nil
}

func (m *k8sMonitor) checkWorkloadsZeroReady(ctx context.Context, now time.Time) (state.CheckResult, error) {
	listOpts := metav1.ListOptions{}
	if m.check != "" {
		listOpts.LabelSelector = m.check
	}

	workloads, err := m.listWorkloadReadiness(ctx, listOpts)
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckError,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	var zeroReady []string
	nodeStatus := map[string]string{}
	for _, w := range workloads {
		if m.exemptSingleReplicaDeploymentsInZeroReady && w.kind == "deploy" && w.desired == 1 {
			continue
		}
		if w.desired > 0 && w.ready == 0 {
			zeroReady = append(zeroReady, w.messageWithNodes(m.workloadNodeSummary(ctx, w, nodeStatus)))
		}
	}

	anyZeroReady := len(zeroReady) > 0
	var msg string
	if anyZeroReady {
		msg = fmt.Sprintf("zero ready: %s", strings.Join(zeroReady, ", "))
	} else {
		msg = "no zero-ready workloads"
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       !anyZeroReady,
		Message:     msg,
		Timestamp:   now,
	}, nil
}

func (m *k8sMonitor) checkWorkloadsPartiallyReady(ctx context.Context, now time.Time) (state.CheckResult, error) {
	listOpts := metav1.ListOptions{}
	if m.check != "" {
		listOpts.LabelSelector = m.check
	}

	workloads, err := m.listWorkloadReadiness(ctx, listOpts)
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckError,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	var partiallyReady []string
	nodeStatus := map[string]string{}
	for _, w := range workloads {
		if w.desired > 0 && w.ready > 0 && w.ready < w.desired {
			partiallyReady = append(partiallyReady, w.messageWithNodes(m.workloadNodeSummary(ctx, w, nodeStatus)))
		}
	}

	anyPartiallyReady := len(partiallyReady) > 0
	var msg string
	if anyPartiallyReady {
		msg = fmt.Sprintf("partially ready: %s", strings.Join(partiallyReady, ", "))
	} else {
		msg = "no partially-ready workloads"
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       !anyPartiallyReady,
		Message:     msg,
		Timestamp:   now,
	}, nil
}
