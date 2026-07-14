package storage

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestS3ClientUploadAndDownload(t *testing.T) {
	client, err := NewS3Client("http://localhost:9000", "us-east-1", "minioadmin", "minioadmin", "pergo-bucket", true)
	if err != nil {
		t.Fatalf("failed to create s3 client: %v", err)
	}

	t.Run("upload and download roundtrip", func(t *testing.T) {
		ctx := context.Background()
		key := "workspace1/hash123.png"
		data := []byte("fake-binary-content")
		contentType := "image/png"

		// Mock client uses global state, clear it first
		// In a real S3 tests we'd write to MinIO, but our mock is in-memory
		err = client.Upload(ctx, key, data, contentType)
		if err != nil {
			t.Fatalf("failed to upload: %v", err)
		}

		body, ct, err := client.Download(ctx, key)
		if err != nil {
			t.Fatalf("failed to download: %v", err)
		}
		defer body.Close()

		retrieved, err := io.ReadAll(body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}

		if string(retrieved) != string(data) {
			t.Errorf("expected %s, got %s", string(data), string(retrieved))
		}
		if ct != contentType {
			t.Errorf("expected %s, got %s", contentType, ct)
		}
	})

	t.Run("download not found returns NoSuchKey", func(t *testing.T) {
		ctx := context.Background()
		_, _, err := client.Download(ctx, "workspace1/nonexistent.png")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var noSuchKey *types.NoSuchKey
		if !errors.As(err, &noSuchKey) {
			t.Errorf("expected NoSuchKey error, got %v", err)
		}
	})
}
