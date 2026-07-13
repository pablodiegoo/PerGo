package outbound_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
)

// fakeQueueDepthTracker tracks depths in memory.
type fakeQueueDepthTracker struct {
	exceeds   bool
	increment uuid.UUID
}

func (f *fakeQueueDepthTracker) Exceeds(workspaceID uuid.UUID, limit int64) bool {
	return f.exceeds
}

func (f *fakeQueueDepthTracker) Increment(workspaceID uuid.UUID) {
	f.increment = workspaceID
}

// fakeMediaUploader tracks uploads.
type fakeMediaUploader struct {
	uploaded []struct {
		key         string
		data        []byte
		contentType string
	}
	err error
}

func (f *fakeMediaUploader) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	if f.err != nil {
		return f.err
	}
	f.uploaded = append(f.uploaded, struct {
		key         string
		data        []byte
		contentType string
	}{key, data, contentType})
	return nil
}

// fakeRouteResolver mocks connection lookups.
type fakeRouteResolver struct {
	conn *repository.Connection
	err  error
}

func (f *fakeRouteResolver) GetBySenderIdentity(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.conn, nil
}

func (f *fakeRouteResolver) GetDefaultChannelConnection(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.conn, nil
}

// fakePublisher tracks NATS publishes.
type fakePublisher struct {
	published [][]byte
	err       error
}

func (f *fakePublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, data)
	return nil
}

func TestProcessor_Ingest(t *testing.T) {
	wsID := uuid.New()
	connID := uuid.New()
	traceID := "test-trace-123"

	defaultConn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		Name:           "Test Telegram Connection",
		Channel:        "telegram",
		SenderIdentity: "@my_bot",
		Status:         "connected",
	}

	t.Run("Ingest standard text message succeeds", func(t *testing.T) {
		tracker := &fakeQueueDepthTracker{}
		uploader := &fakeMediaUploader{}
		resolver := &fakeRouteResolver{conn: defaultConn}
		publisher := &fakePublisher{}

		p := outbound.NewProcessor(tracker, uploader, resolver, publisher)

		req := &domain.CreateMessageRequest{
			To:      "123456",
			Channel: "telegram",
			Body:    "Hello PerGo!",
		}

		qMsg, err := p.Ingest(context.Background(), wsID, traceID, req)
		if err != nil {
			t.Fatalf("Ingest failed: %v", err)
		}

		if qMsg.Body != "Hello PerGo!" {
			t.Errorf("got Body %q, want %q", qMsg.Body, "Hello PerGo!")
		}
		if qMsg.ConnectionID != connID {
			t.Errorf("got ConnectionID %s, want %s", qMsg.ConnectionID, connID)
		}
		if len(publisher.published) != 1 {
			t.Errorf("expected 1 published message, got %d", len(publisher.published))
		}
		if tracker.increment != wsID {
			t.Errorf("expected queue tracker to increment for %s", wsID)
		}
	})

	t.Run("Queue full triggers ErrQueueFull", func(t *testing.T) {
		tracker := &fakeQueueDepthTracker{exceeds: true}
		resolver := &fakeRouteResolver{conn: defaultConn}

		p := outbound.NewProcessor(tracker, nil, resolver, nil)

		req := &domain.CreateMessageRequest{
			To:      "123456",
			Channel: "telegram",
			Body:    "Hold on",
		}

		_, err := p.Ingest(context.Background(), wsID, traceID, req)
		if !errors.Is(err, outbound.ErrQueueFull) {
			t.Errorf("got error %v, want ErrQueueFull", err)
		}
	})

	t.Run("Validation fails wraps structural error", func(t *testing.T) {
		p := outbound.NewProcessor(nil, nil, nil, nil)

		req := &domain.CreateMessageRequest{
			To:      "", // Missing To field triggers validation error
			Channel: "telegram",
		}

		_, err := p.Ingest(context.Background(), wsID, traceID, req)
		var valErr *outbound.ValidationError
		if !errors.As(err, &valErr) {
			t.Fatalf("expected ValidationError, got %v", err)
		}
		if valErr.Response.Details[0].Field != "to" {
			t.Errorf("expected validation failure on 'to' field")
		}
	})

	t.Run("Media size limit exceeded triggers MediaError", func(t *testing.T) {
		uploader := &fakeMediaUploader{}
		resolver := &fakeRouteResolver{conn: defaultConn}

		p := outbound.NewProcessor(nil, uploader, resolver, nil)
		p.SetDownloader(func(ctx context.Context, url string, maxBytes int64) (*storage.DownloadResult, error) {
			return nil, storage.ErrMediaSizeExceeded
		})

		req := &domain.CreateMessageRequest{
			To:      "123456",
			Channel: "telegram",
			Body:    "Image",
			Media: &domain.Media{
				MediaURL:  "https://example.com/huge.png",
				MediaType: "image",
			},
		}

		_, err := p.Ingest(context.Background(), wsID, traceID, req)
		var mediaErr *outbound.MediaError
		if !errors.As(err, &mediaErr) {
			t.Fatalf("expected MediaError, got %v", err)
		}
		if mediaErr.Code != "media_size_exceeded" {
			t.Errorf("got media error code %s, want media_size_exceeded", mediaErr.Code)
		}
	})

	t.Run("Successful media caching uploads to S3 and updates URL", func(t *testing.T) {
		uploader := &fakeMediaUploader{}
		resolver := &fakeRouteResolver{conn: defaultConn}
		publisher := &fakePublisher{}

		p := outbound.NewProcessor(nil, uploader, resolver, publisher)
		p.SetDownloader(func(ctx context.Context, url string, maxBytes int64) (*storage.DownloadResult, error) {
			return &storage.DownloadResult{
				Bytes:       []byte("png-file-bytes"),
				Hash:        "abcde12345",
				ContentType: "image/png",
				Extension:   "png",
			}, nil
		})

		req := &domain.CreateMessageRequest{
			To:      "123456",
			Channel: "telegram",
			Body:    "Image",
			Media: &domain.Media{
				MediaURL:  "https://example.com/sunset.png",
				MediaType: "image",
			},
		}

		qMsg, err := p.Ingest(context.Background(), wsID, traceID, req)
		if err != nil {
			t.Fatalf("Ingest failed: %v", err)
		}

		// Verify S3 Upload
		if len(uploader.uploaded) != 1 {
			t.Fatalf("expected 1 upload, got %d", len(uploader.uploaded))
		}
		if uploader.uploaded[0].key != wsID.String()+"/abcde12345.png" {
			t.Errorf("got upload key %s", uploader.uploaded[0].key)
		}

		// Verify rewired URL
		expectedURL := "/media/" + wsID.String() + "/abcde12345.png"
		if qMsg.Media.MediaURL != expectedURL {
			t.Errorf("got rewired MediaURL %s, want %s", qMsg.Media.MediaURL, expectedURL)
		}
	})

	t.Run("Missing route triggers RouteError", func(t *testing.T) {
		resolver := &fakeRouteResolver{err: errors.New("connection not found")}

		p := outbound.NewProcessor(nil, nil, resolver, nil)

		req := &domain.CreateMessageRequest{
			To:      "123456",
			Channel: "telegram",
			Body:    "Routing check",
		}

		_, err := p.Ingest(context.Background(), wsID, traceID, req)
		var routeErr *outbound.RouteError
		if !errors.As(err, &routeErr) {
			t.Errorf("got error %v, want RouteError", err)
		}
	})
}
