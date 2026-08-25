// Package oss adapts Aliyun Object Storage Service to the
// resource.ObjectStore port — the only place this platform talks to a
// specific object storage vendor. Skill zip uploads are the only current
// caller (spec-05a); nothing else needs multi-cloud abstraction, so this
// package is Aliyun-specific rather than a wrapper over a generic
// interface no second implementation exists for yet.
package oss

import (
	"context"
	"fmt"
	"io"

	aliyunoss "github.com/aliyun/aliyun-oss-go-sdk/oss"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// Store implements resource.ObjectStore against one Aliyun OSS bucket.
type Store struct {
	bucket *aliyunoss.Bucket
}

// New connects to endpoint with the given credentials and resolves bucket.
// Fails fast (a bad endpoint/credential/bucket name is a startup-time
// configuration error, not something to discover on the first upload).
func New(endpoint, accessKeyID, accessKeySecret, bucketName string) (*Store, error) {
	client, err := aliyunoss.New(endpoint, accessKeyID, accessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("oss: new client: %w", err)
	}
	bucket, err := client.Bucket(bucketName)
	if err != nil {
		return nil, fmt.Errorf("oss: bucket %q: %w", bucketName, err)
	}
	return &Store{bucket: bucket}, nil
}

var _ resource.ObjectStore = (*Store)(nil)

func (s *Store) Put(ctx context.Context, key string, r io.Reader, contentType string) error {
	opts := []aliyunoss.Option{aliyunoss.WithContext(ctx)}
	if contentType != "" {
		opts = append(opts, aliyunoss.ContentType(contentType))
	}
	if err := s.bucket.PutObject(key, r, opts...); err != nil {
		return fmt.Errorf("oss: put %q: %w", key, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := s.bucket.GetObject(key, aliyunoss.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("oss: get %q: %w", key, err)
	}
	return rc, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.bucket.DeleteObject(key, aliyunoss.WithContext(ctx)); err != nil {
		return fmt.Errorf("oss: delete %q: %w", key, err)
	}
	return nil
}
