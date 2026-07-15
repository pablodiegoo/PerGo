package media_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/platform/storage"
)

func TestMediaEngine_Download(t *testing.T) {
	// Setup test http server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/small":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 'd', 'a', 't', 'a'})
		case "/exact-limit":
			// 25,000,000 bytes
			data := make([]byte, 25000000)
			for i := range data {
				data[i] = 'A'
			}
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(data)
		case "/exceeds-limit":
			// 25,000,001 bytes
			data := make([]byte, 25000001)
			for i := range data {
				data[i] = 'B'
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(data)
		case "/no-content-type":
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) // Sniffs as image/png
		case "/headers":
			if r.Header.Get("Authorization") == "Bearer secret-token" {
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}
		case "/timeout":
			time.Sleep(100 * time.Millisecond) // short sleep, we will use context timeout to test
			_, _ = w.Write([]byte("too-late"))
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	engine := media.NewDefaultEngine(nil)

	t.Run("successful download and type sniffer", func(t *testing.T) {
		res, err := engine.Download(context.Background(), server.URL+"/small", nil, 25000000)
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
		res, err := engine.Download(context.Background(), server.URL+"/exact-limit", nil, 25000000)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if int64(len(res.Bytes)) != 25000000 {
			t.Errorf("expected 25000000 bytes, got %d", len(res.Bytes))
		}
	})

	t.Run("exceeds the limit (25MB + 1)", func(t *testing.T) {
		_, err := engine.Download(context.Background(), server.URL+"/exceeds-limit", nil, 25000000)
		if !errors.Is(err, media.ErrMediaSizeExceeded) {
			t.Fatalf("expected ErrMediaSizeExceeded, got %v", err)
		}
	})

	t.Run("sniff from content header omission", func(t *testing.T) {
		res, err := engine.Download(context.Background(), server.URL+"/no-content-type", nil, 25000000)
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

	t.Run("headers delivery", func(t *testing.T) {
		headers := map[string]string{
			"Authorization": "Bearer secret-token",
		}
		res, err := engine.Download(context.Background(), server.URL+"/headers", headers, 25000000)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res.ContentType != "image/png" {
			t.Errorf("expected image/png, got %s", res.ContentType)
		}
	})

	t.Run("not found returns error", func(t *testing.T) {
		_, err := engine.Download(context.Background(), server.URL+"/notfound", nil, 25000000)
		if err == nil {
			t.Fatal("expected error for 404, got nil")
		}
	})

	t.Run("context cancellation / timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		_, err := engine.Download(ctx, server.URL+"/timeout", nil, 25000000)
		if err == nil {
			t.Fatal("expected error for timeout, got nil")
		}
	})
}

func TestDefaultEngine_Process(t *testing.T) {
	s3Client, err := storage.NewS3Client("http://localhost:9000", "us-east-1", "minioadmin", "minioadmin", "pergo-bucket", true)
	if err != nil {
		t.Fatalf("failed to create s3 client: %v", err)
	}

	engine := media.NewDefaultEngine(s3Client)
	ctx := context.Background()
	wsID := uuid.New()

	t.Run("ProcessInbound success", func(t *testing.T) {
		data := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
		url, err := engine.ProcessInbound(ctx, wsID, "image", data)
		if err != nil {
			t.Fatalf("ProcessInbound failed: %v", err)
		}

		expectedHash := sha256.New()
		expectedHash.Write(data)
		hashStr := hex.EncodeToString(expectedHash.Sum(nil))

		expectedURL := "/media/" + wsID.String() + "/" + hashStr + ".jpg"
		if url != expectedURL {
			t.Errorf("expected URL %s, got %s", expectedURL, url)
		}
	})

	t.Run("ProcessInbound size limit exceeded", func(t *testing.T) {
		largeData := make([]byte, 25*1024*1024+1)
		_, err := engine.ProcessInbound(ctx, wsID, "image", largeData)
		if !errors.Is(err, media.ErrMediaSizeExceeded) {
			t.Errorf("expected ErrMediaSizeExceeded, got %v", err)
		}
	})

	t.Run("ProcessOutbound success", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 't', 'e', 's', 't'})
		})
		server := httptest.NewServer(handler)
		defer server.Close()

		url, err := engine.ProcessOutbound(ctx, wsID, server.URL)
		if err != nil {
			t.Fatalf("ProcessOutbound failed: %v", err)
		}

		expectedHash := sha256.New()
		expectedHash.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 't', 'e', 's', 't'})
		hashStr := hex.EncodeToString(expectedHash.Sum(nil))

		expectedURL := "/media/" + wsID.String() + "/" + hashStr + ".png"
		if url != expectedURL {
			t.Errorf("expected URL %s, got %s", expectedURL, url)
		}
	})
}
