package investigate

import (
	"context"
	"encoding/json"
	"testing"

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
	if !contains(result.Content, "web-abc") {
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
	if !contains(result.Content, "production") {
		t.Errorf("expected namespace in result: %s", result.Content)
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
	if !contains(result.Content, "OOMKilled") {
		t.Errorf("expected event reason in result: %s", result.Content)
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
	if !contains(result.Content, "not available") {
		t.Errorf("expected 'not available' message: %s", result.Content)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
