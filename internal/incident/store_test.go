package incident

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// fakeS3 is an in-memory s3API for testing.
type fakeS3 struct {
	mu      sync.RWMutex
	objects map[string][]byte
}

func newFakeS3() *fakeS3 { return &fakeS3{objects: make(map[string][]byte)} }

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	data, _ := io.ReadAll(in.Body)
	f.mu.Lock()
	f.objects[*in.Key] = data
	f.mu.Unlock()
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.mu.RLock()
	data, ok := f.objects[*in.Key]
	f.mu.RUnlock()
	if !ok {
		return nil, &fakeNotFound{key: *in.Key}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(data)))}, nil
}

type fakeNotFound struct{ key string }

func (e *fakeNotFound) Error() string { return "NoSuchKey: " + e.key }

func newTestStore() (*S3Store, *fakeS3) {
	fs := newFakeS3()
	return NewS3Store(fs, "test-bucket", "test/"), fs
}

func TestWriteAndReadIncident(t *testing.T) {
	store, _ := newTestStore()
	ctx := context.Background()

	inc := &Incident{
		ID:                  "inc-test-20260510-143000-abcd1234",
		MonitorName:         "api-latency",
		FiredAt:             time.Now().UTC().Truncate(time.Second),
		Status:              StatusActive,
		InvestigationStatus: InvPending,
		Value:               "842ms",
		Message:             "p99 above threshold",
	}

	if err := store.WriteIncident(ctx, inc); err != nil {
		t.Fatalf("WriteIncident: %v", err)
	}

	got, err := store.ReadIncident(ctx, inc.ID)
	if err != nil {
		t.Fatalf("ReadIncident: %v", err)
	}
	if got.ID != inc.ID || got.MonitorName != inc.MonitorName {
		t.Errorf("read incident mismatch: got %+v", got)
	}
}

func TestAppendContent(t *testing.T) {
	store, fs := newTestStore()
	ctx := context.Background()

	inc := &Incident{ID: "inc-x", MonitorName: "x", FiredAt: time.Now(), Status: StatusActive, InvestigationStatus: InvPending}
	store.WriteIncident(ctx, inc)

	store.AppendContent(ctx, inc.ID, "chunk1")
	store.AppendContent(ctx, inc.ID, "chunk2")

	key := store.mdKey(inc.ID)
	fs.mu.RLock()
	content := string(fs.objects[key])
	fs.mu.RUnlock()

	if !strings.Contains(content, "chunk1") || !strings.Contains(content, "chunk2") {
		t.Errorf("expected appended content, got: %s", content)
	}
}

func TestListAndUpdateIndex(t *testing.T) {
	store, _ := newTestStore()
	ctx := context.Background()

	// Empty index
	idx, err := store.ListIncidents(ctx)
	if err != nil {
		t.Fatalf("ListIncidents on empty: %v", err)
	}
	if len(idx.Incidents) != 0 {
		t.Error("expected empty index")
	}

	sum := IncidentSummary{ID: "inc-1", MonitorName: "m1", FiredAt: time.Now(), Status: StatusActive, InvestigationStatus: InvPending}
	store.UpdateIndex(ctx, sum)

	idx, _ = store.ListIncidents(ctx)
	if len(idx.Incidents) != 1 || idx.Incidents[0].ID != "inc-1" {
		t.Errorf("expected 1 incident, got %+v", idx.Incidents)
	}

	// Update existing
	sum.InvestigationStatus = InvComplete
	store.UpdateIndex(ctx, sum)
	idx, _ = store.ListIncidents(ctx)
	if len(idx.Incidents) != 1 || idx.Incidents[0].InvestigationStatus != InvComplete {
		t.Error("expected updated investigation status")
	}
}
