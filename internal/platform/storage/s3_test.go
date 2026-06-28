package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestDownloadAndValidate(t *testing.T) {
	// Setup test http server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/small":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 'd', 'a', 't', 'a'})
		case "/exact-limit":
			// 25,000,000 bytes
			data := make([]byte, 25000000)
			for i := range data {
				data[i] = 'A'
			}
			w.Header().Set("Content-Type", "application/pdf")
			w.Write(data)
		case "/exceeds-limit":
			// 25,000,001 bytes
			data := make([]byte, 25000001)
			for i := range data {
				data[i] = 'B'
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.Write(data)
		case "/no-content-type":
			w.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) // Sniffs as image/png
		case "/timeout":
			time.Sleep(100 * time.Millisecond) // short sleep, we will use context timeout to test
			w.Write([]byte("too-late"))
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	t.Run("successful download and type sniffer", func(t *testing.T) {
		res, err := DownloadAndValidate(context.Background(), server.URL+"/small", 25000000)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		expectedBytes := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 'd', 'a', 't', 'a'}
		if string(res.Bytes) != string(expectedBytes) {
			t.Errorf("expected payload, got %v", res.Bytes)
		}
		if res.ContentType != "image/png" {
			t.Errorf("expected ContentType image/png, got %s", res.ContentType)
		}
		if res.Extension != "png" {
			t.Errorf("expected Extension png, got %s", res.Extension)
		}

		expectedHash := sha256.New()
		expectedHash.Write(expectedBytes)
		hashStr := hex.EncodeToString(expectedHash.Sum(nil))
		if res.Hash != hashStr {
			t.Errorf("expected Hash %s, got %s", hashStr, res.Hash)
		}
	})

	t.Run("exactly at the limit (25MB)", func(t *testing.T) {
		res, err := DownloadAndValidate(context.Background(), server.URL+"/exact-limit", 25000000)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if int64(len(res.Bytes)) != 25000000 {
			t.Errorf("expected 25000000 bytes, got %d", len(res.Bytes))
		}
	})

	t.Run("exceeds the limit (25MB + 1)", func(t *testing.T) {
		_, err := DownloadAndValidate(context.Background(), server.URL+"/exceeds-limit", 25000000)
		if !errors.Is(err, ErrMediaSizeExceeded) {
			t.Fatalf("expected ErrMediaSizeExceeded, got %v", err)
		}
	})

	t.Run("sniff from content header omission", func(t *testing.T) {
		res, err := DownloadAndValidate(context.Background(), server.URL+"/no-content-type", 25000000)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res.ContentType != "image/png" {
			t.Errorf("expected sniffed image/png, got %s", res.ContentType)
		}
		if res.Extension != "png" {
			t.Errorf("expected extension png, got %s", res.Extension)
		}
	})

	t.Run("not found returns error", func(t *testing.T) {
		_, err := DownloadAndValidate(context.Background(), server.URL+"/notfound", 25000000)
		if err == nil {
			t.Fatal("expected error for 404, got nil")
		}
	})

	t.Run("context cancellation / timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		_, err := DownloadAndValidate(ctx, server.URL+"/timeout", 25000000)
		if err == nil {
			t.Fatal("expected error for timeout, got nil")
		}
	})
}

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
