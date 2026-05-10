package incident

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Store is the persistence interface for incidents.
type Store interface {
	// WriteIncident writes the incident metadata (JSON) and initial markdown to S3.
	WriteIncident(ctx context.Context, inc *Incident) error
	// ReadIncident reads incident metadata by ID.
	ReadIncident(ctx context.Context, id string) (*Incident, error)
	// ReadContent reads the full markdown content of an incident.
	ReadContent(ctx context.Context, id string) (string, error)
	// AppendContent appends text to the incident's markdown file.
	AppendContent(ctx context.Context, id, content string) error
	// ListIncidents returns the index of all incidents.
	ListIncidents(ctx context.Context) (Index, error)
	// UpdateIndex upserts an IncidentSummary in the index.
	UpdateIndex(ctx context.Context, summary IncidentSummary) error
}

// s3API is the subset of the AWS S3 client we use (enables testing with fakes).
type s3API interface {
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// S3Store implements Store using AWS S3.
type S3Store struct {
	client s3API
	bucket string
	prefix string // e.g. "incidents/"
}

// NewS3Store creates an S3Store. prefix should end with "/" or be empty.
func NewS3Store(client s3API, bucket, prefix string) *S3Store {
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &S3Store{client: client, bucket: bucket, prefix: prefix}
}

func (s *S3Store) metaKey(id string) string {
	return s.prefix + "incidents/" + id + "/incident.json"
}

func (s *S3Store) mdKey(id string) string {
	return s.prefix + "incidents/" + id + "/incident.md"
}

func (s *S3Store) indexKey() string {
	return s.prefix + "index.json"
}

func (s *S3Store) put(ctx context.Context, key, contentType string, body []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	})
	return err
}

func (s *S3Store) get(ctx context.Context, key string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (s *S3Store) WriteIncident(ctx context.Context, inc *Incident) error {
	meta, err := json.Marshal(inc)
	if err != nil {
		return fmt.Errorf("marshal incident: %w", err)
	}
	if err := s.put(ctx, s.metaKey(inc.ID), "application/json", meta); err != nil {
		return fmt.Errorf("write incident meta: %w", err)
	}
	md := []byte(inc.InitialMarkdown())
	if err := s.put(ctx, s.mdKey(inc.ID), "text/markdown", md); err != nil {
		return fmt.Errorf("write incident markdown: %w", err)
	}
	return nil
}

func (s *S3Store) ReadIncident(ctx context.Context, id string) (*Incident, error) {
	data, err := s.get(ctx, s.metaKey(id))
	if err != nil {
		return nil, fmt.Errorf("read incident %s: %w", id, err)
	}
	var inc Incident
	if err := json.Unmarshal(data, &inc); err != nil {
		return nil, fmt.Errorf("unmarshal incident %s: %w", id, err)
	}
	return &inc, nil
}

// ReadContent returns the full markdown content of an incident from S3.
func (s *S3Store) ReadContent(ctx context.Context, id string) (string, error) {
	data, err := s.get(ctx, s.mdKey(id))
	if err != nil {
		return "", fmt.Errorf("read content %s: %w", id, err)
	}
	return string(data), nil
}

// AppendContent reads the existing markdown, appends content, and rewrites it.
func (s *S3Store) AppendContent(ctx context.Context, id, content string) error {
	existing, err := s.get(ctx, s.mdKey(id))
	if err != nil {
		existing = []byte{}
	}
	updated := append(existing, []byte(content)...)
	return s.put(ctx, s.mdKey(id), "text/markdown", updated)
}

func (s *S3Store) ListIncidents(ctx context.Context) (Index, error) {
	data, err := s.get(ctx, s.indexKey())
	if err != nil {
		// NoSuchKey — return empty index
		var notFound *types.NoSuchKey
		if isNotFound(err, &notFound) {
			return Index{Incidents: []IncidentSummary{}}, nil
		}
		return Index{}, fmt.Errorf("read index: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return Index{}, fmt.Errorf("unmarshal index: %w", err)
	}
	if idx.Incidents == nil {
		idx.Incidents = []IncidentSummary{}
	}
	return idx, nil
}

func (s *S3Store) UpdateIndex(ctx context.Context, summary IncidentSummary) error {
	idx, err := s.ListIncidents(ctx)
	if err != nil {
		return err
	}
	updated := false
	for i, existing := range idx.Incidents {
		if existing.ID == summary.ID {
			idx.Incidents[i] = summary
			updated = true
			break
		}
	}
	if !updated {
		// Prepend so newest is first
		idx.Incidents = append([]IncidentSummary{summary}, idx.Incidents...)
	}
	data, err := json.Marshal(idx)
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	return s.put(ctx, s.indexKey(), "application/json", data)
}

// isNotFound checks if err wraps a specific S3 not-found error type.
func isNotFound(err error, target interface{}) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "NoSuchKey") ||
		strings.Contains(err.Error(), "no such key") ||
		strings.Contains(err.Error(), "404")
}
