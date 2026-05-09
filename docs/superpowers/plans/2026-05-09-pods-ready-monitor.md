# pods_ready Monitor Kind Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `kind: pods_ready` to the kubernetes monitor type, which checks pod Ready conditions directly instead of relying on stale Warning events.

**Architecture:** The new check kind lists pods in a namespace (optionally filtered by label selector via the `check` config field), skips completed/terminating pods, and returns `Value: bool` — `true` if any pod is NotReady. This slots into the existing `k8sMonitor.Check` switch with no changes to interfaces, registry, engine, or state. The `check` config field is made optional so `pods_ready` configs don't need to specify it.

**Tech Stack:** Go, `k8s.io/client-go`, `k8s.io/client-go/kubernetes/fake` for tests.

---

## Files

- Modify: `internal/monitor/kubernetes/kubernetes.go` — make `check` optional, add `pods_ready` case and `checkPodsReady` method
- Modify: `internal/monitor/kubernetes/kubernetes_test.go` — add tests for `pods_ready`

---

### Task 1: Implement `kind: pods_ready`

**Files:**
- Modify: `internal/monitor/kubernetes/kubernetes.go`
- Modify: `internal/monitor/kubernetes/kubernetes_test.go`

- [ ] **Step 1: Write the failing tests**

Add these tests at the end of `internal/monitor/kubernetes/kubernetes_test.go`:

```go
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
	// All skipped — no active unready pods.
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
```

Note: you also need `"strings"` in the test file imports.

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra
go test ./internal/monitor/kubernetes/... -run "TestPodsReady" -v
```

Expected: compile error or FAIL — `pods_ready` case not yet implemented.

- [ ] **Step 3: Make `check` optional and add the `pods_ready` case in `kubernetes.go`**

In `NewWithClient`, change the `check` parsing from required to optional:

```go
// Before:
check, err := cfgString(cfg, "check", true)
if err != nil {
    return nil, fmt.Errorf("kubernetes monitor %q: %w", name, err)
}

// After:
check, _ := cfgString(cfg, "check", false)
```

Add the new case in the `Check` switch (after the `"event"` case, before `default`):

```go
case "pods_ready":
    return m.checkPodsReady(ctx, now)
```

Add the `checkPodsReady` method at the end of the file:

```go
// checkPodsReady lists pods in the configured namespace, skipping completed and
// terminating pods, and returns Value=true if any active pod is not Ready.
// The check field is used as an optional label selector.
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
```

- [ ] **Step 4: Run all kubernetes monitor tests**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra
go test ./internal/monitor/kubernetes/... -v
```

Expected: all tests PASS, including the 4 new `TestPodsReady*` tests and the existing `TestDeploymentReadyReplicas` and `TestNodeReadyCondition`.

- [ ] **Step 5: Run full test suite**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/monitor/kubernetes/kubernetes.go internal/monitor/kubernetes/kubernetes_test.go
git commit -m "feat: add kind: pods_ready kubernetes monitor"
```

---

### Task 2: Release and update config

**Files:**
- Modify: `deploy/helm/klyra/Chart.yaml` — bump version
- Modify: `/Users/michelfeldheim/Github/terraform-infra/argocd/infrastructure/kubernetes/klyra/values.yaml` — replace event-based pod-unhealthy with pods_ready
- Modify: `/Users/michelfeldheim/Github/terraform-infra/argocd/infrastructure/kubernetes/klyra/kustomization.yaml` — bump chart version
- Modify: `/Users/michelfeldheim/Github/terraform-infra/argocd/infrastructure/kubernetes/klyra/values.yaml` — bump image tag

- [ ] **Step 1: Bump Chart.yaml version**

In `deploy/helm/klyra/Chart.yaml`, change (current version is whichever was last released — check the file):

```yaml
version: 0.1.24
appVersion: "0.1.24"
```

- [ ] **Step 2: Commit and tag**

```bash
git add deploy/helm/klyra/Chart.yaml
git commit -m "chore: bump chart to 0.1.24"
git tag v0.1.24
git push && git push --tags
```

Wait for CI to complete (GitHub Actions builds and pushes the image and helm chart).

- [ ] **Step 3: Update terraform-infra**

In `/Users/michelfeldheim/Github/terraform-infra/argocd/infrastructure/kubernetes/klyra/values.yaml`, replace the `pod-unhealthy` monitor:

```yaml
# REMOVE this monitor entirely:
    - name: pod-unhealthy
      type: kubernetes
      interval: 15s
      group: global-infra
      icon: kubernetes
      priority: high
      config:
        kind: event
        check: type=Warning
        name: Unhealthy
        window: 2m
      threshold:
        operator: eq
        value: true
        for: 60s
        recovery_for: 60s
      actions:
        - pushover-alert

# REPLACE with:
    - name: pod-unhealthy
      type: kubernetes
      interval: 15s
      group: global-infra
      icon: kubernetes
      priority: high
      config:
        kind: pods_ready
        namespace: ""
      threshold:
        operator: eq
        value: true
        for: 60s
        recovery_for: 60s
      actions:
        - pushover-alert
```

Also update image tag and chart version in `values.yaml` and `kustomization.yaml` to `0.1.24`.

- [ ] **Step 4: Commit and push terraform-infra**

```bash
cd /Users/michelfeldheim/Github/terraform-infra
git add argocd/infrastructure/kubernetes/klyra/
git commit -m "feat(klyra): switch pod-unhealthy to pods_ready kind (0.1.24)"
git push
```

ArgoCD will auto-sync and deploy the new config.
