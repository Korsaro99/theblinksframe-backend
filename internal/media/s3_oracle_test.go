//go:build oracle

package media

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

// TestOracle runs the S3 storage against the real Oracle Object Storage buckets, to check the
// set-up in deploy/README.md ("Media"): the key works, a photo is public by its URL, originals
// are not, and nobody can list the public bucket. It writes and then deletes a few bytes under
// products/oracle-check/. Run it with `make test-oracle` (MEDIA_S3_* from .env); it is never
// part of CI, because it needs real credentials and the internet.
func TestOracle(t *testing.T) {
	cfg := S3Config{
		Endpoint:        os.Getenv("MEDIA_S3_ENDPOINT"),
		Region:          os.Getenv("MEDIA_S3_REGION"),
		AccessKeyID:     os.Getenv("MEDIA_S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("MEDIA_S3_SECRET_ACCESS_KEY"),
		Bucket:          os.Getenv("MEDIA_S3_BUCKET"),
		OriginalsBucket: os.Getenv("MEDIA_S3_ORIGINALS_BUCKET"),
		PublicBaseURL:   os.Getenv("MEDIA_PUBLIC_BASE_URL"),
	}
	for name, v := range map[string]string{
		"MEDIA_S3_ENDPOINT": cfg.Endpoint, "MEDIA_S3_REGION": cfg.Region,
		"MEDIA_S3_ACCESS_KEY_ID": cfg.AccessKeyID, "MEDIA_S3_SECRET_ACCESS_KEY": cfg.SecretAccessKey,
		"MEDIA_S3_BUCKET": cfg.Bucket, "MEDIA_S3_ORIGINALS_BUCKET": cfg.OriginalsBucket,
		"MEDIA_PUBLIC_BASE_URL": cfg.PublicBaseURL,
	} {
		if v == "" {
			t.Fatalf("%s is not set (see .env.example, Media)", name)
		}
	}
	s, err := NewS3(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	prefix := "products/oracle-check/" + time.Now().UTC().Format("20060102T150405")
	t.Cleanup(func() {
		if err := s.DeletePrefix(context.Background(), prefix); err != nil {
			t.Errorf("clean up %s: %v (delete it in the console)", prefix, err)
		}
	})

	// 1. The key can write to both buckets.
	if err := s.Put(ctx, prefix+"/w400.jpg", "image/jpeg", []byte("oracle check")); err != nil {
		t.Fatalf("put in %s: %v (check the key, endpoint, region and the blinks-media policy)", cfg.Bucket, err)
	}
	if err := s.PutOriginal(ctx, prefix+"/original", "image/jpeg", []byte("private")); err != nil {
		t.Fatalf("put in %s: %v", cfg.OriginalsBucket, err)
	}

	// 2. Anyone can load a public photo by its URL, as customers' browsers will.
	status, body, header := get(t, s.URL(prefix+"/w400.jpg"))
	if status != http.StatusOK || body != "oracle check" {
		t.Errorf("public URL %s: %d %q; want 200 (is blinks-media's visibility Public?)", s.URL(prefix+"/w400.jpg"), status, body)
	}
	if ct := header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}

	// 3. Originals (which may hold GPS locations) are not readable without the key.
	originalURL := strings.Replace(s.URL(prefix+"/original"), "/b/"+cfg.Bucket+"/", "/b/"+cfg.OriginalsBucket+"/", 1)
	if status, _, _ := get(t, originalURL); status == http.StatusOK {
		t.Errorf("anyone can read %s: make blinks-media-originals Private", originalURL)
	}
	// ...but the key can, for making new sizes later.
	obj, err := s.client.GetObject(ctx, cfg.OriginalsBucket, prefix+"/original", minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	got, err := io.ReadAll(obj)
	_ = obj.Close()
	if err != nil || string(got) != "private" {
		t.Errorf("original = %q, %v", got, err)
	}

	// 4. Nobody can list the public bucket ("Allow users to list objects" unticked).
	listURL := strings.TrimSuffix(strings.TrimRight(cfg.PublicBaseURL, "/"), "/o") + "/o"
	if status, _, _ := get(t, listURL); status == http.StatusOK {
		t.Errorf("anyone can list %s: untick \"Allow users to list objects\"", cfg.Bucket)
	}

	// 5. A wrong secret is refused.
	wrong := cfg
	wrong.SecretAccessKey = "not-the-secret"
	bad, _ := NewS3(wrong)
	if err := bad.Put(ctx, prefix+"/wrong.jpg", "image/jpeg", []byte("x")); err == nil {
		t.Error("a wrong secret was accepted")
	}
}

// get fetches url with no credentials, like a visitor's browser.
func get(t *testing.T, url string) (int, string, http.Header) {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec,noctx // test-only; url comes from our own config
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode, string(b), resp.Header
}
