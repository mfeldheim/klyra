package k8smon_test

import (
	"context"
	"strings"
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
	if r.MonitorName != "test" {
		t.Errorf("expected MonitorName=test, got %q", r.MonitorName)
	}
	if r.Timestamp.IsZero() {
		t.Error("expected non-zero Timestamp")
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

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != true {
		t.Errorf("expected ready_condition=true, got %v", r.Value)
	}
	if r.MonitorName != "test" {
		t.Errorf("expected MonitorName=test, got %q", r.MonitorName)
	}
	if r.Timestamp.IsZero() {
		t.Error("expected non-zero Timestamp")
	}
}

func TestPodsReadyAllReady(t *testing.T) {
	client := fake.NewSimpleClientset()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	client.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})

	m, err := k8smon.NewWithClient("test", map[string]any{
		"kind":      "pods_ready",
		"namespace": "default",
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
	if r.Value != false {
		t.Errorf("expected Value=false (all ready), got %v", r.Value)
	}
}

func TestPodsReadySomeNotReady(t *testing.T) {
	client := fake.NewSimpleClientset()
	readyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	notReadyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-2", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
		},
	}
	client.CoreV1().Pods("default").Create(context.Background(), readyPod, metav1.CreateOptions{})
	client.CoreV1().Pods("default").Create(context.Background(), notReadyPod, metav1.CreateOptions{})

	m, err := k8smon.NewWithClient("test", map[string]any{
		"kind":      "pods_ready",
		"namespace": "default",
	}, client)
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != true {
		t.Errorf("expected Value=true (some not ready), got %v", r.Value)
	}
	if !strings.Contains(r.Message, "web-2") {
		t.Errorf("expected not-ready pod name in message, got %q", r.Message)
	}
}

func TestPodsReadySkipsCompletedAndTerminating(t *testing.T) {
	client := fake.NewSimpleClientset()
	now := metav1.Now()

	succeededPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	failedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "job-2", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed},
	}
	terminatingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "web-3",
			Namespace:         "default",
			DeletionTimestamp: &now,
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	client.CoreV1().Pods("default").Create(context.Background(), succeededPod, metav1.CreateOptions{})
	client.CoreV1().Pods("default").Create(context.Background(), failedPod, metav1.CreateOptions{})
	client.CoreV1().Pods("default").Create(context.Background(), terminatingPod, metav1.CreateOptions{})

	m, err := k8smon.NewWithClient("test", map[string]any{
		"kind":      "pods_ready",
		"namespace": "default",
	}, client)
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != false {
		t.Errorf("expected Value=false (completed/terminating skipped), got %v: %s", r.Value, r.Message)
	}
}

func TestPodsReadyLabelSelector(t *testing.T) {
	client := fake.NewSimpleClientset()
	matchingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-1",
			Namespace: "default",
			Labels:    map[string]string{"app": "api"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
		},
	}
	otherPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-1",
			Namespace: "default",
			Labels:    map[string]string{"app": "worker"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	client.CoreV1().Pods("default").Create(context.Background(), matchingPod, metav1.CreateOptions{})
	client.CoreV1().Pods("default").Create(context.Background(), otherPod, metav1.CreateOptions{})

	m, err := k8smon.NewWithClient("test", map[string]any{
		"kind":      "pods_ready",
		"namespace": "default",
		"check":     "app=api",
	}, client)
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != true {
		t.Errorf("expected Value=true (api pod not ready), got %v", r.Value)
	}
}

func TestPodsReadyEmptyNamespace(t *testing.T) {
	client := fake.NewSimpleClientset()

	m, err := k8smon.NewWithClient("test", map[string]any{
		"kind":      "pods_ready",
		"namespace": "default",
	}, client)
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != false {
		t.Errorf("expected Value=false (no pods), got %v", r.Value)
	}
	if r.Message != "all pods ready" {
		t.Errorf("expected 'all pods ready', got %q", r.Message)
	}
}
