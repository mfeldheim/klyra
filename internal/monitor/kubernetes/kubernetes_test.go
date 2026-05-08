package k8smon_test

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	k8smon "github.com/mfeldheim/klyra/internal/monitor/kubernetes"
	"github.com/mfeldheim/klyra/internal/state"
)

func TestDeploymentReadyReplicas(t *testing.T) {
	client := fake.NewSimpleClientset()
	ready := int32(2)
	client.AppsV1().Deployments("default").Create(context.Background(), &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: ready,
		},
	}, metav1.CreateOptions{})

	m, err := k8smon.NewWithClient("test", map[string]any{
		"kind":      "deployment",
		"namespace": "default",
		"name":      "api",
		"check":     "ready_replicas",
	}, client)
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != state.CheckOK {
		t.Errorf("expected OK, got %s: %s", r.Status, r.Message)
	}
	if r.Value != float64(2) {
		t.Errorf("expected ready_replicas=2, got %v", r.Value)
	}
}

func TestNodeReadyCondition(t *testing.T) {
	client := fake.NewSimpleClientset()
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
	client.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	m, err := k8smon.NewWithClient("test", map[string]any{
		"kind":  "node",
		"name":  "node-1",
		"check": "ready_condition",
	}, client)
	if err != nil {
		t.Fatal(err)
	}

	r, _ := m.Check(context.Background())
	if r.Value != true {
		t.Errorf("expected ready_condition=true, got %v", r.Value)
	}
}
