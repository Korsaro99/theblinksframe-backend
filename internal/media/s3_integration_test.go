//go:build integration

package media

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestS3 runs the S3 storage against SeaweedFS, an S3-compatible server, standing in for
// Oracle Object Storage's S3-compatible API. It checks the requests are signed (a wrong
// secret is refused), originals stay out of the public bucket, and DeletePrefix empties both.
// TODO: once the Oracle buckets exist, try it against Oracle itself (deploy/README.md, "Media").
func TestS3(t *testing.T) {
	ctx := context.Background()
	const access, secret = "test-access-key", "test-secret-key-that-is-long-enough"
	config := `{"identities":[{"name":"test","credentials":[{"accessKey":"` + access + `","secretKey":"` + secret +
		`"}],"actions":["Admin","Read","Write","List","Tagging"]}]}`
	ctr, err := testcontainers.Run(ctx, "chrislusf/seaweedfs:3.97",
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader: bytes.NewReader([]byte(config)), ContainerFilePath: "/etc/seaweedfs/s3.json", FileMode: 0o644,
		}),
		testcontainers.WithCmd("server", "-dir=/data", "-s3", "-s3.port=8333", "-s3.config=/etc/seaweedfs/s3.json"),
		testcontainers.WithExposedPorts("8333/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("8333/tcp").WithStartupTimeout(2*time.Minute)),
	)
	if ctr != nil {
		t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	}
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := ctr.PortEndpoint(ctx, "8333/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}

	cfg := S3Config{
		Endpoint: endpoint, Region: "us-east-1", AccessKeyID: access, SecretAccessKey: secret,
		Bucket: "blinks-media", OriginalsBucket: "blinks-media-originals",
		PublicBaseURL: "https://objectstorage.example/n/ns/b/blinks-media/o/",
	}
	s, err := NewS3(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// SeaweedFS needs a moment after its port opens before it accepts bucket requests.
	for _, b := range []string{cfg.Bucket, cfg.OriginalsBucket} {
		var err error
		for range 30 {
			if err = s.client.MakeBucket(ctx, b, minio.MakeBucketOptions{Region: cfg.Region}); err == nil {
				break
			}
			time.Sleep(time.Second)
		}
		if err != nil {
			t.Fatalf("make bucket %s: %v", b, err)
		}
	}

	prefix := "products/p1/m1"
	if err := s.Put(ctx, prefix+"/w400.jpg", "image/jpeg", []byte("small")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := s.PutOriginal(ctx, prefix+"/original", "image/jpeg", []byte("with gps")); err != nil {
		t.Fatalf("put original: %v", err)
	}
	read := func(bucket, key string) (string, error) {
		obj, err := s.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
		if err != nil {
			return "", err
		}
		defer obj.Close()
		b, err := io.ReadAll(obj)
		return string(b), err
	}
	if got, err := read(cfg.Bucket, prefix+"/w400.jpg"); err != nil || got != "small" {
		t.Errorf("public file = %q, %v", got, err)
	}
	if info, err := s.client.StatObject(ctx, cfg.Bucket, prefix+"/w400.jpg", minio.StatObjectOptions{}); err != nil ||
		info.ContentType != "image/jpeg" {
		t.Errorf("content type = %q, %v", info.ContentType, err)
	}
	if _, err := read(cfg.Bucket, prefix+"/original"); err == nil {
		t.Error("the original is in the public bucket")
	}
	if got, _ := read(cfg.OriginalsBucket, prefix+"/original"); got != "with gps" {
		t.Errorf("original = %q", got)
	}
	if got := s.URL(prefix + "/w400.jpg"); got != "https://objectstorage.example/n/ns/b/blinks-media/o/products/p1/m1/w400.jpg" {
		t.Errorf("URL = %s", got)
	}

	if err := s.DeletePrefix(ctx, prefix); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, b := range []string{cfg.Bucket, cfg.OriginalsBucket} {
		for obj := range s.client.ListObjects(ctx, b, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			t.Errorf("left in %s after DeletePrefix: %s %v", b, obj.Key, obj.Err)
		}
	}

	wrong := cfg
	wrong.SecretAccessKey = "not-the-secret"
	bad, _ := NewS3(wrong)
	if err := bad.Put(ctx, "products/x/y/w400.jpg", "image/jpeg", []byte("x")); err == nil {
		t.Error("a wrong secret was accepted: requests are not being signed or checked")
	}
}
