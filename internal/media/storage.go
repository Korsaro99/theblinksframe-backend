package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Storage keeps media files: the photos and videos customers see in a public place, and the
// originals as uploaded in a private one, for re-making sizes later. Keys look like
// "products/<productId>/<mediaId>/w800.jpg".
type Storage interface {
	// Put stores a file customers may see.
	Put(ctx context.Context, key, contentType string, data []byte) error
	// PutOriginal stores the upload as it came, privately: a phone photo's original can
	// hold its GPS location. Storage without a private place may skip it.
	PutOriginal(ctx context.Context, key, contentType string, data []byte) error
	// DeletePrefix deletes every file, public and original, whose key starts with prefix.
	DeletePrefix(ctx context.Context, prefix string) error
	// URL is where customers load a public file from.
	URL(key string) string
}

// S3Config reaches S3-compatible storage. Oracle Object Storage's endpoint looks like
// https://<namespace>.compat.objectstorage.<region>.oraclecloud.com.
type S3Config struct {
	Endpoint        string // https://host, or http:// for a local test server
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string // public-read: what customers load
	OriginalsBucket string // private; empty means originals are not kept
	PublicBaseURL   string // e.g. https://objectstorage.<region>.oraclecloud.com/n/<namespace>/b/<bucket>/o
}

// S3 stores media in S3-compatible buckets, such as Oracle Object Storage.
type S3 struct {
	client *minio.Client
	cfg    S3Config
}

var _ Storage = (*S3)(nil)

// NewS3 returns S3 storage. It does not contact the server.
func NewS3(cfg S3Config) (*S3, error) {
	host, secure := strings.CutPrefix(cfg.Endpoint, "https://")
	if !secure {
		host = strings.TrimPrefix(cfg.Endpoint, "http://")
	}
	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: secure,
		Region: cfg.Region,
		// Oracle's S3 endpoint only understands bucket names in the path.
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("media storage: %w", err)
	}
	return &S3{client: client, cfg: cfg}, nil
}

// Put implements Storage.
func (s *S3) Put(ctx context.Context, key, contentType string, data []byte) error {
	return s.put(ctx, s.cfg.Bucket, key, contentType, data, "public, max-age=31536000, immutable")
}

// PutOriginal implements Storage.
func (s *S3) PutOriginal(ctx context.Context, key, contentType string, data []byte) error {
	if s.cfg.OriginalsBucket == "" {
		return nil
	}
	return s.put(ctx, s.cfg.OriginalsBucket, key, contentType, data, "private, no-store")
}

func (s *S3) put(ctx context.Context, bucket, key, contentType string, data []byte, cache string) error {
	_, err := s.client.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType, CacheControl: cache,
		// One plain PUT with a known size: the most widely supported request, which Oracle's
		// S3 layer accepts.
		DisableMultipart: true,
	})
	if err != nil {
		return fmt.Errorf("store %s: %w", key, err)
	}
	return nil
}

// DeletePrefix implements Storage.
func (s *S3) DeletePrefix(ctx context.Context, prefix string) error {
	var errs []error
	for _, bucket := range []string{s.cfg.Bucket, s.cfg.OriginalsBucket} {
		if bucket == "" {
			continue
		}
		for obj := range s.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if obj.Err != nil {
				errs = append(errs, obj.Err)
				break
			}
			if err := s.client.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
				errs = append(errs, fmt.Errorf("delete %s: %w", obj.Key, err))
			}
		}
	}
	return errors.Join(errs...)
}

// URL implements Storage.
func (s *S3) URL(key string) string {
	return strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/" + key
}

// Local stores media in a folder for development; the API serves the public part at baseURL.
// Originals go to a sibling folder that is never served.
type Local struct {
	dir     string
	baseURL string
}

var _ Storage = (*Local)(nil)

// NewLocal returns folder storage under dir: dir/public is served, dir/originals is not.
func NewLocal(dir, baseURL string) *Local { return &Local{dir: dir, baseURL: baseURL} }

// PublicDir is the folder to serve at the base URL.
func (l *Local) PublicDir() string { return filepath.Join(l.dir, "public") }

// Put implements Storage.
func (l *Local) Put(_ context.Context, key, _ string, data []byte) error {
	return l.write(filepath.Join(l.dir, "public"), key, data)
}

// PutOriginal implements Storage.
func (l *Local) PutOriginal(_ context.Context, key, _ string, data []byte) error {
	return l.write(filepath.Join(l.dir, "originals"), key, data)
}

func (l *Local) write(root, key string, data []byte) error {
	path, err := safeJoin(root, key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("media folder: %w", err)
	}
	return os.WriteFile(path, data, 0o600) // the API serves them itself, so only it reads them
}

// DeletePrefix implements Storage. Prefixes are whole folders ("products/<id>/<id>").
func (l *Local) DeletePrefix(_ context.Context, prefix string) error {
	var errs []error
	for _, root := range []string{"public", "originals"} {
		path, err := safeJoin(filepath.Join(l.dir, root), prefix)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// URL implements Storage.
func (l *Local) URL(key string) string { return strings.TrimRight(l.baseURL, "/") + "/" + key }

// safeJoin joins key under root and refuses anything that would climb out of it. Keys are
// made by this package from UUIDs, so this is a second line of defence.
func safeJoin(root, key string) (string, error) {
	path := filepath.Join(root, filepath.FromSlash(key))
	if rel, err := filepath.Rel(root, path); err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("media key %q is outside the media folder", key)
	}
	return path, nil
}

// Memory keeps media in memory, for tests.
type Memory struct {
	mu        sync.Mutex
	Public    map[string][]byte
	Originals map[string][]byte
}

var _ Storage = (*Memory)(nil)

// NewMemory returns empty memory storage.
func NewMemory() *Memory {
	return &Memory{Public: map[string][]byte{}, Originals: map[string][]byte{}}
}

// Put implements Storage.
func (m *Memory) Put(_ context.Context, key, _ string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Public[key] = data
	return nil
}

// PutOriginal implements Storage.
func (m *Memory) PutOriginal(_ context.Context, key, _ string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Originals[key] = data
	return nil
}

// DeletePrefix implements Storage.
func (m *Memory) DeletePrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, files := range []map[string][]byte{m.Public, m.Originals} {
		for k := range files {
			if strings.HasPrefix(k, prefix) {
				delete(files, k)
			}
		}
	}
	return nil
}

// URL implements Storage.
func (m *Memory) URL(key string) string { return "https://media.test/" + key }

// Keys lists the public keys, sorted, for tests.
func (m *Memory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.Public))
	for k := range m.Public {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
