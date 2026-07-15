package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/storage"
)

// mockPublisher is already defined in message_test.go, but this test file is in the same package,
// so we don't redefine it. Wait, the package has mockPublisher defined.

func TestMessageHandler_CreateWithMedia(t *testing.T) {
	// Initialize S3 Mock storage client
	// (Since we replaces the sdk with our local mock in-memory client, it's safe to run in sandbox)
	s3Client, err := storage.NewS3Client("http://localhost:9000", "us-east-1", "minioadmin", "minioadmin", "pergo-bucket", true)
	if err != nil {
		t.Fatalf("failed to init s3 client: %v", err)
	}

	e := echo.New()
	pub := &mockPublisher{}
	h := &MessageHandler{
		Publisher:      pub,
		S3Client:       s3Client,
		ConnectionRepo: defaultMockConnectionRepo(),
	}
	h.RegisterRoutes(e)

	// Mock server for remote media download
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 'd', 'a', 't', 'a'})
		case "/large.png":
			data := make([]byte, 25*1024*1024+1)
			w.Header().Set("Content-Type", "image/png")
			w.Write(data)
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer downloadServer.Close()

	wsID := uuid.New()

	t.Run("successful media download and ingest", func(t *testing.T) {
		body := `{"to":"+1234567890","channel":"whatsapp","media":{"media_url":"` + downloadServer.URL + `/image.png","media_type":"image"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(testContext(uuid.New().String(), wsID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp domain.CreateMessageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}

		// Verify NATS queue payload has media URL rewired
		var qMsg domain.QueueMessage
		if err := json.Unmarshal(pub.data, &qMsg); err != nil {
			t.Fatalf("failed to unmarshal published data: %v", err)
		}

		if qMsg.Media == nil {
			t.Fatal("expected media to be populated in QueueMessage")
		}

		expectedPrefix := "/media/" + wsID.String() + "/"
		if !strings.HasPrefix(qMsg.Media.MediaURL, expectedPrefix) {
			t.Errorf("expected rewired media URL starting with %s, got %s", expectedPrefix, qMsg.Media.MediaURL)
		}

		// Verify S3 object is accessible via the rewired key
		// Rewired URL is /media/{workspace_id}/{hash}.{ext}
		parts := strings.Split(qMsg.Media.MediaURL, "/")
		hashWithExt := parts[len(parts)-1]
		key := wsID.String() + "/" + hashWithExt

		rc, ct, err := s3Client.Download(context.Background(), key)
		if err != nil {
			t.Fatalf("expected S3 object to exist at key %s, got err %v", key, err)
		}
		defer rc.Close()

		if ct != "image/png" {
			t.Errorf("expected S3 contentType image/png, got %s", ct)
		}
	})

	t.Run("media size exceeded rejection", func(t *testing.T) {
		body := `{"to":"+1234567890","channel":"whatsapp","media":{"media_url":"` + downloadServer.URL + `/large.png","media_type":"image"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(testContext(uuid.New().String(), wsID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp domain.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}

		if errResp.Code != "media_size_exceeded" {
			t.Errorf("expected error code media_size_exceeded, got %s", errResp.Code)
		}
	})

	t.Run("media download failure rejection", func(t *testing.T) {
		body := `{"to":"+1234567890","channel":"whatsapp","media":{"media_url":"` + downloadServer.URL + `/notfound","media_type":"image"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(testContext(uuid.New().String(), wsID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp domain.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}

		if errResp.Code != "media_download_failed" {
			t.Errorf("expected error code media_download_failed, got %s", errResp.Code)
		}
	})
}
