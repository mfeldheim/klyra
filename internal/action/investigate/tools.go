package investigate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ToolResult is the output from a tool execution.
type ToolResult struct {
	Content string
	IsError bool
}

// toolDef is the JSON schema definition sent to Bedrock.
type toolDef struct {
	Name        string
	Description string
	Schema      map[string]any
}

// K8sTools executes read-only Kubernetes tool calls on behalf of the agent.
type K8sTools struct {
	client  kubernetes.Interface
	metrics metricsv.Interface // may be nil if metrics-server not available
}

// NewK8sTools creates a K8sTools. metricsClient may be nil.
func NewK8sTools(client kubernetes.Interface, metricsClient metricsv.Interface) *K8sTools {
	return &K8sTools{client: client, metrics: metricsClient}
}

// Definitions returns the tool schema list to pass to Bedrock.
func Definitions() []toolDef {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	strOpt := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	boolOpt := func(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
	intOpt := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

	return []toolDef{
		{
			Name:        "list_namespaces",
			Description: "List all Kubernetes namespaces.",
			Schema:      map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "list_pods",
			Description: "List pods in a namespace with their status, restart count, node, and age.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace":      str("Kubernetes namespace"),
				"label_selector": strOpt("Optional label selector, e.g. app=foo"),
			}, "required": []string{"namespace"}},
		},
		{
			Name:        "describe_pod",
			Description: "Get detailed info about a pod including spec, status, conditions, and recent events.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace": str("Kubernetes namespace"),
				"name":      str("Pod name"),
			}, "required": []string{"namespace", "name"}},
		},
		{
			Name:        "get_pod_logs",
			Description: "Get the last N lines of logs from a pod container (max 200 lines).",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace": str("Kubernetes namespace"),
				"name":      str("Pod name"),
				"container": strOpt("Container name (defaults to first container)"),
				"lines":     intOpt("Number of lines to return (default 100, max 200)"),
				"previous":  boolOpt("If true, return logs from previous terminated container"),
			}, "required": []string{"namespace", "name"}},
		},
		{
			Name:        "list_events",
			Description: "List events in a namespace, warnings first. Optionally filter by involved object name.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace":       str("Kubernetes namespace"),
				"involved_object": strOpt("Filter by involved object name"),
			}, "required": []string{"namespace"}},
		},
		{
			Name:        "list_deployments",
			Description: "List deployments in a namespace with desired/ready/available replica counts.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace": str("Kubernetes namespace"),
			}, "required": []string{"namespace"}},
		},
		{
			Name:        "describe_deployment",
			Description: "Get detailed info about a deployment including rollout status.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace": str("Kubernetes namespace"),
				"name":      str("Deployment name"),
			}, "required": []string{"namespace", "name"}},
		},
		{
			Name:        "list_replicasets",
			Description: "List ReplicaSets in a namespace.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace":  str("Kubernetes namespace"),
				"deployment": strOpt("Filter by owning deployment name"),
			}, "required": []string{"namespace"}},
		},
		{
			Name:        "list_nodes",
			Description: "List all nodes with status, roles, conditions, and taints.",
			Schema:      map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "describe_node",
			Description: "Get detailed info about a node including allocatable resources and conditions.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"name": str("Node name"),
			}, "required": []string{"name"}},
		},
		{
			Name:        "list_daemonsets",
			Description: "List DaemonSets in a namespace.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace": str("Kubernetes namespace"),
			}, "required": []string{"namespace"}},
		},
		{
			Name:        "list_statefulsets",
			Description: "List StatefulSets in a namespace.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace": str("Kubernetes namespace"),
			}, "required": []string{"namespace"}},
		},
		{
			Name:        "list_services",
			Description: "List services in a namespace.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace": str("Kubernetes namespace"),
			}, "required": []string{"namespace"}},
		},
		{
			Name:        "list_hpa",
			Description: "List HorizontalPodAutoscalers in a namespace with current/min/max replicas.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace": str("Kubernetes namespace"),
			}, "required": []string{"namespace"}},
		},
		{
			Name:        "get_pod_metrics",
			Description: "Get current CPU and memory usage for a specific pod.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace": str("Kubernetes namespace"),
				"name":      str("Pod name"),
			}, "required": []string{"namespace", "name"}},
		},
		{
			Name:        "list_pod_metrics",
			Description: "Get current CPU and memory usage for all pods in a namespace.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"namespace": str("Kubernetes namespace"),
			}, "required": []string{"namespace"}},
		},
		{
			Name:        "list_node_metrics",
			Description: "Get current CPU and memory usage for all nodes.",
			Schema:      map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

func parseInput(raw json.RawMessage) map[string]any {
	var m map[string]any
	json.Unmarshal(raw, &m) //nolint:errcheck
	return m
}

func str(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// Execute dispatches a tool call by name and returns the result.
func (t *K8sTools) Execute(ctx context.Context, name string, input json.RawMessage) ToolResult {
	m := parseInput(input)
	switch name {
	case "list_namespaces":
		return t.listNamespaces(ctx)
	case "list_pods":
		return t.listPods(ctx, str(m, "namespace"), str(m, "label_selector"))
	case "describe_pod":
		return t.describePod(ctx, str(m, "namespace"), str(m, "name"))
	case "get_pod_logs":
		lines := int64(100)
		if v, ok := m["lines"]; ok {
			if f, ok := v.(float64); ok && f > 0 {
				lines = int64(f)
				if lines > 200 {
					lines = 200
				}
			}
		}
		previous := false
		if v, ok := m["previous"]; ok {
			if b, ok := v.(bool); ok {
				previous = b
			}
		}
		return t.getPodLogs(ctx, str(m, "namespace"), str(m, "name"), str(m, "container"), lines, previous)
	case "list_events":
		return t.listEvents(ctx, str(m, "namespace"), str(m, "involved_object"))
	case "list_deployments":
		return t.listDeployments(ctx, str(m, "namespace"))
	case "describe_deployment":
		return t.describeDeployment(ctx, str(m, "namespace"), str(m, "name"))
	case "list_replicasets":
		return t.listReplicaSets(ctx, str(m, "namespace"), str(m, "deployment"))
	case "list_nodes":
		return t.listNodes(ctx)
	case "describe_node":
		return t.describeNode(ctx, str(m, "name"))
	case "list_daemonsets":
		return t.listDaemonSets(ctx, str(m, "namespace"))
	case "list_statefulsets":
		return t.listStatefulSets(ctx, str(m, "namespace"))
	case "list_services":
		return t.listServices(ctx, str(m, "namespace"))
	case "list_hpa":
		return t.listHPA(ctx, str(m, "namespace"))
	case "get_pod_metrics":
		return t.getPodMetrics(ctx, str(m, "namespace"), str(m, "name"))
	case "list_pod_metrics":
		return t.listPodMetrics(ctx, str(m, "namespace"))
	case "list_node_metrics":
		return t.listNodeMetrics(ctx)
	default:
		return ToolResult{Content: fmt.Sprintf("unknown tool: %s", name), IsError: true}
	}
}

func ok(content string) ToolResult { return ToolResult{Content: content} }
func errResult(err error) ToolResult {
	return ToolResult{Content: err.Error(), IsError: true}
}

func (t *K8sTools) listNamespaces(ctx context.Context) ToolResult {
	list, err := t.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	for _, ns := range list.Items {
		fmt.Fprintf(&sb, "%s (status: %s)\n", ns.Name, ns.Status.Phase)
	}
	return ok(sb.String())
}

func (t *K8sTools) listPods(ctx context.Context, namespace, selector string) ToolResult {
	opts := metav1.ListOptions{LabelSelector: selector}
	list, err := t.client.CoreV1().Pods(namespace).List(ctx, opts)
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-40s %-12s %-8s %-8s %s\n", "NAME", "PHASE", "READY", "RESTARTS", "NODE")
	for _, pod := range list.Items {
		ready := 0
		restarts := int32(0)
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Ready {
				ready++
			}
			restarts += cs.RestartCount
		}
		fmt.Fprintf(&sb, "%-40s %-12s %d/%-6d %-8d %s\n",
			pod.Name, pod.Status.Phase, ready, len(pod.Spec.Containers), restarts, pod.Spec.NodeName)
	}
	return ok(sb.String())
}

func (t *K8sTools) describePod(ctx context.Context, namespace, name string) ToolResult {
	pod, err := t.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Name: %s\nNamespace: %s\nNode: %s\nPhase: %s\n",
		pod.Name, pod.Namespace, pod.Spec.NodeName, pod.Status.Phase)
	sb.WriteString("\nContainers:\n")
	for _, cs := range pod.Status.ContainerStatuses {
		fmt.Fprintf(&sb, "  %s: ready=%v restarts=%d\n", cs.Name, cs.Ready, cs.RestartCount)
		if cs.State.Waiting != nil {
			fmt.Fprintf(&sb, "    Waiting: %s — %s\n", cs.State.Waiting.Reason, cs.State.Waiting.Message)
		}
		if cs.State.Terminated != nil {
			fmt.Fprintf(&sb, "    Terminated: %s (exit %d)\n", cs.State.Terminated.Reason, cs.State.Terminated.ExitCode)
		}
	}
	sb.WriteString("\nConditions:\n")
	for _, c := range pod.Status.Conditions {
		fmt.Fprintf(&sb, "  %s: %s\n", c.Type, c.Status)
	}
	return ok(sb.String())
}

func (t *K8sTools) getPodLogs(ctx context.Context, namespace, name, container string, lines int64, previous bool) ToolResult {
	opts := &corev1.PodLogOptions{TailLines: &lines, Previous: previous}
	if container != "" {
		opts.Container = container
	}
	req := t.client.CoreV1().Pods(namespace).GetLogs(name, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return errResult(err)
	}
	defer stream.Close()
	var sb strings.Builder
	buf := make([]byte, 32*1024)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return ok(sb.String())
}

func (t *K8sTools) listEvents(ctx context.Context, namespace, involvedObject string) ToolResult {
	opts := metav1.ListOptions{}
	if involvedObject != "" {
		opts.FieldSelector = "involvedObject.name=" + involvedObject
	}
	list, err := t.client.CoreV1().Events(namespace).List(ctx, opts)
	if err != nil {
		return errResult(err)
	}
	// Sort: warnings first
	var warnings, normals []corev1.Event
	for _, e := range list.Items {
		if e.Type == corev1.EventTypeWarning {
			warnings = append(warnings, e)
		} else {
			normals = append(normals, e)
		}
	}
	events := append(warnings, normals...)
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-8s %-40s %-25s %s\n", "TYPE", "OBJECT", "REASON", "MESSAGE")
	for _, e := range events {
		obj := fmt.Sprintf("%s/%s", e.InvolvedObject.Kind, e.InvolvedObject.Name)
		msg := e.Message
		if len(msg) > 80 {
			msg = msg[:80] + "..."
		}
		fmt.Fprintf(&sb, "%-8s %-40s %-25s %s\n", e.Type, obj, e.Reason, msg)
	}
	return ok(sb.String())
}

func (t *K8sTools) listDeployments(ctx context.Context, namespace string) ToolResult {
	list, err := t.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-40s %-8s %-8s %-8s\n", "NAME", "DESIRED", "READY", "AVAILABLE")
	for _, d := range list.Items {
		fmt.Fprintf(&sb, "%-40s %-8d %-8d %-8d\n",
			d.Name, *d.Spec.Replicas, d.Status.ReadyReplicas, d.Status.AvailableReplicas)
	}
	return ok(sb.String())
}

func (t *K8sTools) describeDeployment(ctx context.Context, namespace, name string) ToolResult {
	d, err := t.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Name: %s\nNamespace: %s\nDesired: %d\nReady: %d\nAvailable: %d\nUpdated: %d\n",
		d.Name, d.Namespace, *d.Spec.Replicas, d.Status.ReadyReplicas, d.Status.AvailableReplicas, d.Status.UpdatedReplicas)
	sb.WriteString("\nConditions:\n")
	for _, c := range d.Status.Conditions {
		fmt.Fprintf(&sb, "  %s: %s — %s\n", c.Type, c.Status, c.Message)
	}
	return ok(sb.String())
}

func (t *K8sTools) listReplicaSets(ctx context.Context, namespace, deployment string) ToolResult {
	list, err := t.client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	for _, rs := range list.Items {
		if deployment != "" {
			owned := false
			for _, ref := range rs.OwnerReferences {
				if ref.Kind == "Deployment" && ref.Name == deployment {
					owned = true
				}
			}
			if !owned {
				continue
			}
		}
		fmt.Fprintf(&sb, "%s: desired=%d ready=%d\n", rs.Name, *rs.Spec.Replicas, rs.Status.ReadyReplicas)
	}
	return ok(sb.String())
}

func (t *K8sTools) listNodes(ctx context.Context) ToolResult {
	list, err := t.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	for _, n := range list.Items {
		ready := "Unknown"
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady {
				ready = string(c.Status)
			}
		}
		fmt.Fprintf(&sb, "%s: Ready=%s\n", n.Name, ready)
	}
	return ok(sb.String())
}

func (t *K8sTools) describeNode(ctx context.Context, name string) ToolResult {
	n, err := t.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Name: %s\n", n.Name)
	sb.WriteString("Allocatable:\n")
	for k, v := range n.Status.Allocatable {
		fmt.Fprintf(&sb, "  %s: %s\n", k, v.String())
	}
	sb.WriteString("Conditions:\n")
	for _, c := range n.Status.Conditions {
		fmt.Fprintf(&sb, "  %s: %s\n", c.Type, c.Status)
	}
	return ok(sb.String())
}

func (t *K8sTools) listDaemonSets(ctx context.Context, namespace string) ToolResult {
	list, err := t.client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	for _, ds := range list.Items {
		fmt.Fprintf(&sb, "%s: desired=%d ready=%d available=%d\n",
			ds.Name, ds.Status.DesiredNumberScheduled, ds.Status.NumberReady, ds.Status.NumberAvailable)
	}
	return ok(sb.String())
}

func (t *K8sTools) listStatefulSets(ctx context.Context, namespace string) ToolResult {
	list, err := t.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	for _, ss := range list.Items {
		fmt.Fprintf(&sb, "%s: desired=%d ready=%d\n", ss.Name, *ss.Spec.Replicas, ss.Status.ReadyReplicas)
	}
	return ok(sb.String())
}

func (t *K8sTools) listServices(ctx context.Context, namespace string) ToolResult {
	list, err := t.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-40s %-12s %-16s %s\n", "NAME", "TYPE", "CLUSTER-IP", "PORTS")
	for _, svc := range list.Items {
		ports := make([]string, 0, len(svc.Spec.Ports))
		for _, p := range svc.Spec.Ports {
			ports = append(ports, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
		}
		fmt.Fprintf(&sb, "%-40s %-12s %-16s %s\n",
			svc.Name, svc.Spec.Type, svc.Spec.ClusterIP, strings.Join(ports, ","))
	}
	return ok(sb.String())
}

func (t *K8sTools) listHPA(ctx context.Context, namespace string) ToolResult {
	list, err := t.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	for _, hpa := range list.Items {
		fmt.Fprintf(&sb, "%s: min=%d max=%d current=%d\n",
			hpa.Name, *hpa.Spec.MinReplicas, hpa.Spec.MaxReplicas, hpa.Status.CurrentReplicas)
	}
	return ok(sb.String())
}

func (t *K8sTools) getPodMetrics(ctx context.Context, namespace, name string) ToolResult {
	if t.metrics == nil {
		return ok("metrics API not available in this cluster")
	}
	m, err := t.metrics.MetricsV1beta1().PodMetricses(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	for _, c := range m.Containers {
		fmt.Fprintf(&sb, "%s: cpu=%s memory=%s\n",
			c.Name, c.Usage.Cpu().String(), c.Usage.Memory().String())
	}
	return ok(sb.String())
}

func (t *K8sTools) listPodMetrics(ctx context.Context, namespace string) ToolResult {
	if t.metrics == nil {
		return ok("metrics API not available in this cluster")
	}
	list, err := t.metrics.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	for _, pm := range list.Items {
		for _, c := range pm.Containers {
			fmt.Fprintf(&sb, "%s/%s: cpu=%s memory=%s\n",
				pm.Name, c.Name, c.Usage.Cpu().String(), c.Usage.Memory().String())
		}
	}
	return ok(sb.String())
}

func (t *K8sTools) listNodeMetrics(ctx context.Context) ToolResult {
	if t.metrics == nil {
		return ok("metrics API not available in this cluster")
	}
	list, err := t.metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return errResult(err)
	}
	var sb strings.Builder
	for _, nm := range list.Items {
		fmt.Fprintf(&sb, "%s: cpu=%s memory=%s\n",
			nm.Name, nm.Usage.Cpu().String(), nm.Usage.Memory().String())
	}
	return ok(sb.String())
}
