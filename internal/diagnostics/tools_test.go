package diagnostics

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestListPods(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-abc", Namespace: "default"},
		Spec:       corev1.PodSpec{NodeName: "node-1"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})
	tools := NewK8sTools(client, nil)
	result := tools.Execute(context.Background(), "list_pods", json.RawMessage(`{"namespace":"default"}`))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "web-abc") {
		t.Errorf("expected pod name in result: %s", result.Content)
	}
}

func TestListNamespaces(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "production"},
	})
	tools := NewK8sTools(client, nil)
	result := tools.Execute(context.Background(), "list_namespaces", json.RawMessage(`{}`))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "production") {
		t.Errorf("expected namespace in result: %s", result.Content)
	}
}

func TestListNodesShowsVersions(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "ip-10-2-0-1"},
		Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{
			OSImage:                 "Bottlerocket OS 1.68.1 (aws-k8s-1.37)",
			KernelVersion:           "6.18.1",
			ContainerRuntimeVersion: "containerd://2.3.2",
			KubeletVersion:          "v1.37.2",
		}},
	})
	tools := NewK8sTools(client, nil)
	for _, call := range []struct{ name, args string }{
		{"list_nodes", `{}`},
		{"describe_node", `{"name":"ip-10-2-0-1"}`},
	} {
		result := tools.Execute(context.Background(), call.name, json.RawMessage(call.args))
		if result.IsError {
			t.Fatalf("%s: unexpected error: %s", call.name, result.Content)
		}
		for _, want := range []string{"Bottlerocket OS 1.68.1", "6.18.1", "containerd://2.3.2"} {
			if !strings.Contains(result.Content, want) {
				t.Errorf("%s: expected %q in result: %s", call.name, want, result.Content)
			}
		}
	}
}

func TestListEvents(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "e1", Namespace: "default"},
		Type:           corev1.EventTypeWarning,
		Reason:         "OOMKilled",
		Message:        "container killed due to OOM",
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web-abc"},
	})
	tools := NewK8sTools(client, nil)
	result := tools.Execute(context.Background(), "list_events", json.RawMessage(`{"namespace":"default"}`))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "OOMKilled") {
		t.Errorf("expected event reason in result: %s", result.Content)
	}
}

// TestListEventsShowsTimestamps guards the event timestamp: newer clients only
// set eventTime/series (firstTimestamp and lastTimestamp stay empty), and
// without a time column incidents cannot be correlated with events.
func TestListEventsShowsTimestamps(t *testing.T) {
	older := time.Date(2026, 11, 18, 13, 50, 0, 0, time.UTC)
	newer := time.Date(2026, 11, 18, 21, 41, 0, 0, time.UTC)
	client := fake.NewSimpleClientset(
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "old", Namespace: "keda"},
			Type:           corev1.EventTypeWarning,
			Reason:         "OldReason",
			LastTimestamp:  metav1.NewTime(older),
			Count:          3,
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "a"},
		},
		&corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "keda"},
			Type:       corev1.EventTypeWarning,
			Reason:     "NewReason",
			EventTime:  metav1.NewMicroTime(newer.Add(-time.Minute)),
			Series: &corev1.EventSeries{
				Count:            7,
				LastObservedTime: metav1.NewMicroTime(newer),
			},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "b"},
		},
	)
	result := NewK8sTools(client, nil).Execute(context.Background(), "list_events", json.RawMessage(`{"namespace":"keda"}`))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	for _, want := range []string{"LAST SEEN", "2026-11-18T13:50:00Z", "2026-11-18T21:41:00Z", "7"} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("expected %q in result:\n%s", want, result.Content)
		}
	}
	if strings.Index(result.Content, "NewReason") > strings.Index(result.Content, "OldReason") {
		t.Errorf("expected newest event first:\n%s", result.Content)
	}
}

func TestUnknownTool(t *testing.T) {
	tools := NewK8sTools(fake.NewSimpleClientset(), nil)
	result := tools.Execute(context.Background(), "does_not_exist", json.RawMessage(`{}`))
	if !result.IsError {
		t.Error("expected error for unknown tool")
	}
}

func TestMetricsUnavailable(t *testing.T) {
	tools := NewK8sTools(fake.NewSimpleClientset(), nil) // nil metrics client
	result := tools.Execute(context.Background(), "get_pod_metrics", json.RawMessage(`{"namespace":"default","name":"pod-1"}`))
	if result.IsError {
		t.Errorf("expected graceful unavailable, got error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "not available") {
		t.Errorf("expected 'not available' message: %s", result.Content)
	}
}

func TestDefinitionsShape(t *testing.T) {
	defs := Definitions()
	if len(defs) == 0 {
		t.Fatal("expected at least one tool definition")
	}
	for _, d := range defs {
		if d.Name == "" {
			t.Error("tool with empty name")
		}
		if d.InputSchema == nil {
			t.Errorf("tool %s has nil input schema", d.Name)
		}
	}
}
