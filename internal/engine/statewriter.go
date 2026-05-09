package engine

import (
	"context"
	"encoding/json"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/mfeldheim/klyra/internal/state"
)

// StateWriter periodically flushes the in-memory store to a Kubernetes ConfigMap.
type StateWriter struct {
	store     *state.Store
	client    kubernetes.Interface
	namespace string
	cmName    string
	interval  time.Duration
}

// NewStateWriter creates a StateWriter with a 10-second flush interval.
func NewStateWriter(st *state.Store, client kubernetes.Interface, namespace, cmName string) *StateWriter {
	return &StateWriter{
		store:     st,
		client:    client,
		namespace: namespace,
		cmName:    cmName,
		interval:  10 * time.Second,
	}
}

// Run ticks every interval and flushes the store to the ConfigMap when dirty.
func (sw *StateWriter) Run(ctx context.Context) {
	ticker := time.NewTicker(sw.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if sw.store.IsDirty() {
				if err := sw.flush(ctx); err != nil {
					log.Printf("statewriter: flush error: %v", err)
					sw.store.SetDirty() // ensure retry on next tick
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// flush snapshots the store and persists it to the ConfigMap.
func (sw *StateWriter) flush(ctx context.Context) error {
	sw.store.ClearDirty()
	ps := sw.store.Snapshot(24 * time.Hour)
	data, err := json.Marshal(ps)
	if err != nil {
		return err
	}

	cms := sw.client.CoreV1().ConfigMaps(sw.namespace)
	existing, err := cms.Get(ctx, sw.cmName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sw.cmName,
				Namespace: sw.namespace,
			},
			Data: map[string]string{
				"state.json": string(data),
			},
		}
		_, err = cms.Create(ctx, cm, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}

	if existing.Data == nil {
		existing.Data = make(map[string]string)
	}
	existing.Data["state.json"] = string(data)
	_, err = cms.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// LoadFromConfigMap reads persisted state from a ConfigMap and loads it into
// the store. It is a no-op when the ConfigMap does not yet exist (first run).
func LoadFromConfigMap(ctx context.Context, st *state.Store, client kubernetes.Interface, namespace, cmName string) error {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	raw, ok := cm.Data["state.json"]
	if !ok || raw == "" {
		return nil
	}

	var ps state.PersistedState
	if err := json.Unmarshal([]byte(raw), &ps); err != nil {
		return err
	}
	st.LoadSnapshot(ps)
	return nil
}
