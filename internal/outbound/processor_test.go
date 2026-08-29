package outbound_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

type fakeMediaEngine struct {
	downloadFn        func(ctx context.Context, url string, headers map[string]string, maxBytes int64) (*media.DownloadResult, error)
	uploadFn          func(ctx context.Context, key string, data []byte, contentType string) error
	processOutboundFn func(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error)
	processInboundFn  func(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error)

	inboundCalls []struct {
		workspaceID uuid.UUID
		mediaType   string
		data        []byte
	}
	outboundCalls []struct {
		workspaceID uuid.UUID
		mediaURL    string
	}
}

func (f *fakeMediaEngine) Download(ctx context.Context, url string, headers map[string]string, maxBytes int64) (*media.DownloadResult, error) {
	if f.downloadFn != nil {
		return f.downloadFn(ctx, url, headers, maxBytes)
	}
	return nil, nil
}

func (f *fakeMediaEngine) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	if f.uploadFn != nil {
		return f.uploadFn(ctx, key, data, contentType)
	}
	return nil
}

func (f *fakeMediaEngine) ProcessOutbound(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error) {
	f.outboundCalls = append(f.outboundCalls, struct {
		workspaceID uuid.UUID
		mediaURL    string
	}{workspaceID, mediaURL})
	if f.processOutboundFn != nil {
		return f.processOutboundFn(ctx, workspaceID, mediaURL)
	}
	return "", nil
}

func (f *fakeMediaEngine) ProcessInbound(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error) {
	f.inboundCalls = append(f.inboundCalls, struct {
		workspaceID uuid.UUID
		mediaType   string
		data        []byte
	}{workspaceID, mediaType, data})
	if f.processInboundFn != nil {
		return f.processInboundFn(ctx, workspaceID, mediaType, data)
	}
	return "", nil
}

func (f *fakeMediaEngine) ExtractAudioTelemetry(data []byte, contentType string) (*media.AudioTelemetry, error) {
	return media.ExtractAudioTelemetry(data, contentType)
}

func (f *fakeMediaEngine) Transcode(ctx context.Context, data []byte, targetMime string) ([]byte, *media.AudioTelemetry, error) {
	return media.TranscodeAudio(data, targetMime)
}

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

// fakeRouteResolver mocks connection lookups.
type fakeRouteResolver struct {
	conn *repository.Connection
	err  error
}

func (f *fakeRouteResolver) GetBySlug(ctx context.Context, workspaceID uuid.UUID, slug string) (*repository.Connection, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.conn != nil && f.conn.Slug != "" && f.conn.Slug == slug {
		return f.conn, nil
	}
	return nil, repository.ErrConnectionNotFound
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
		me := &fakeMediaEngine{}
		resolver := &fakeRouteResolver{conn: defaultConn}
		publisher := &fakePublisher{}

		p := outbound.NewProcessor(tracker, me, resolver, publisher)

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
	})

	t.Run("Ingest message with connection slug succeeds", func(t *testing.T) {
		slugConn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    wsID,
			Name:           "Vendas SP",
			Slug:           "vendas-sp",
			Channel:        "whatsapp",
			SenderIdentity: "5511999990001",
			Status:         "connected",
		}
		tracker := &fakeQueueDepthTracker{}
		me := &fakeMediaEngine{}
		resolver := &fakeRouteResolver{conn: slugConn}
		publisher := &fakePublisher{}

		p := outbound.NewProcessor(tracker, me, resolver, publisher)

		req := &domain.CreateMessageRequest{
			To:      "5511988887777",
			Channel: "vendas-sp",
			Body:    "Olá de Vendas SP!",
		}

		qMsg, err := p.Ingest(context.Background(), wsID, traceID, req)
		if err != nil {
			t.Fatalf("Ingest with slug failed: %v", err)
		}

		if qMsg.Channel != "whatsapp" {
			t.Errorf("got resolved QueueMessage Channel %q, want underlying channel %q", qMsg.Channel, "whatsapp")
		}
		if qMsg.ConnectionID != slugConn.ID {
			t.Errorf("got ConnectionID %s, want %s", qMsg.ConnectionID, slugConn.ID)
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
		me := &fakeMediaEngine{
			processOutboundFn: func(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error) {
				return "", media.ErrMediaSizeExceeded
			},
		}
		resolver := &fakeRouteResolver{conn: defaultConn}

		p := outbound.NewProcessor(nil, me, resolver, nil)

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
		me := &fakeMediaEngine{
			processOutboundFn: func(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error) {
				return "/media/" + workspaceID.String() + "/abcde12345.png", nil
			},
		}
		resolver := &fakeRouteResolver{conn: defaultConn}
		publisher := &fakePublisher{}

		p := outbound.NewProcessor(nil, me, resolver, publisher)

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

		// Verify ProcessOutbound Call
		if len(me.outboundCalls) != 1 {
			t.Fatalf("expected 1 ProcessOutbound call, got %d", len(me.outboundCalls))
		}
		call := me.outboundCalls[0]
		if call.mediaURL != "https://example.com/sunset.png" {
			t.Errorf("got mediaURL %s, want https://example.com/sunset.png", call.mediaURL)
		}

		// Verify rewired URL
		expectedURL := "/media/" + wsID.String() + "/abcde12345.png"
		if qMsg.Media.MediaURL != expectedURL {
			t.Errorf("got rewired MediaURL %s, want %s", qMsg.Media.MediaURL, expectedURL)
		}
	})

	t.Run("Ingest PTT voice note downloads, uploads to S3, preserves PTT flag, and publishes", func(t *testing.T) {
		me := &fakeMediaEngine{
			processOutboundFn: func(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error) {
				return "/media/" + workspaceID.String() + "/voice12345.ogg", nil
			},
		}
		resolver := &fakeRouteResolver{conn: defaultConn}
		publisher := &fakePublisher{}
		tracker := &fakeQueueDepthTracker{}

		p := outbound.NewProcessor(tracker, me, resolver, publisher)

		req := &domain.CreateMessageRequest{
			To:      "123456",
			Channel: "telegram",
			Media: &domain.Media{
				MediaURL:  "https://example.com/audio/voice-sample.ogg",
				MediaType: "voice",
				PTT:       true,
			},
		}

		qMsg, err := p.Ingest(context.Background(), wsID, traceID, req)
		if err != nil {
			t.Fatalf("Ingest failed: %v", err)
		}

		if len(me.outboundCalls) != 1 {
			t.Fatalf("expected 1 ProcessOutbound call, got %d", len(me.outboundCalls))
		}
		if me.outboundCalls[0].mediaURL != "https://example.com/audio/voice-sample.ogg" {
			t.Errorf("got mediaURL %s, want https://example.com/audio/voice-sample.ogg", me.outboundCalls[0].mediaURL)
		}

		expectedURL := "/media/" + wsID.String() + "/voice12345.ogg"
		if qMsg.Media.MediaURL != expectedURL {
			t.Errorf("got rewired MediaURL %s, want %s", qMsg.Media.MediaURL, expectedURL)
		}
		if !qMsg.Media.PTT {
			t.Errorf("expected qMsg.Media.PTT to be true")
		}
		if len(publisher.published) != 1 {
			t.Fatalf("expected 1 published message, got %d", len(publisher.published))
		}

		var publishedMsg domain.QueueMessage
		if err := json.Unmarshal(publisher.published[0], &publishedMsg); err != nil {
			t.Fatalf("failed to unmarshal published queue message: %v", err)
		}
		if publishedMsg.Media == nil || !publishedMsg.Media.PTT {
			t.Errorf("expected published queue message to have PTT=true, got %+v", publishedMsg.Media)
		}
		if publishedMsg.Media.MediaURL != expectedURL {
			t.Errorf("expected published media URL %s, got %s", expectedURL, publishedMsg.Media.MediaURL)
		}
		if tracker.increment != wsID {
			t.Errorf("expected queue tracker to increment for %s", wsID)
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

type fakeSessionReader struct {
	sess *repository.RecipientSession
	err  error
}

func (f *fakeSessionReader) Get(ctx context.Context, key domain.SessionKey) (*repository.RecipientSession, error) {
	return f.sess, f.err
}

func TestProcessor_SessionFallback(t *testing.T) {
	wsID := uuid.New()
	connID := uuid.New()
	traceID := "test-trace-456"

	creds := map[string]string{
		"default_template_name":     "hello_world",
		"default_template_language": "en_US",
	}
	credsJSON, _ := json.Marshal(creds)

	wabaConn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		Name:           "WABA Test",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "123456789",
		Status:         "connected",
		Credentials:    credsJSON,
	}

	tracker := &fakeQueueDepthTracker{}
	me := &fakeMediaEngine{}
	resolver := &fakeRouteResolver{conn: wabaConn}
	publisher := &fakePublisher{}

	p := outbound.NewProcessor(tracker, me, resolver, publisher)

	// Session is closed (older than 24h)
	oldSession := &repository.RecipientSession{
		SessionKey: domain.SessionKey{
			WorkspaceID:       wsID,
			RecipientPhone:    "5511999999999",
			Channel:           "whatsapp_cloud",
			RecipientIdentity: "123456789",
		},
		LastInboundAt:  time.Now().Add(-48 * time.Hour),
		EntryPointType: "standard",
	}
	sessionReader := &fakeSessionReader{sess: oldSession}
	windowChecker := session.NewWindowChecker(sessionReader)
	p.SetWindowChecker(windowChecker)

	req := &domain.CreateMessageRequest{
		To:      "5511999999999",
		Channel: "whatsapp_cloud",
		Body:    "Hello freeform!",
	}

	qMsg, err := p.Ingest(context.Background(), wsID, traceID, req)
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	if qMsg.TemplateName != "hello_world" {
		t.Errorf("got TemplateName %q, want 'hello_world'", qMsg.TemplateName)
	}
	if qMsg.Language != "en_US" {
		t.Errorf("got Language %q, want 'en_US'", qMsg.Language)
	}
	if qMsg.Body != "" {
		t.Errorf("expected Body to be empty, got %q", qMsg.Body)
	}
	if len(qMsg.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(qMsg.Components))
	}
	if qMsg.Components[0].Type != "body" {
		t.Errorf("expected component type 'body', got %q", qMsg.Components[0].Type)
	}
}

func TestProcessor_CatalogResolution(t *testing.T) {
	wsID := uuid.New()
	connID := uuid.New()
	traceID := "trace-catalog-123"

	t.Run("Explicit catalog_id is preserved", func(t *testing.T) {
		conn := &repository.Connection{
			ID:             connID,
			WorkspaceID:    wsID,
			Channel:        "whatsapp_cloud",
			SenderIdentity: "12345",
			Status:         "connected",
		}
		p := outbound.NewProcessor(nil, nil, &fakeRouteResolver{conn: conn}, &fakePublisher{})

		req := &domain.CreateMessageRequest{
			To:      "5511999999999",
			Channel: "whatsapp_cloud",
			Type:    "product",
			Product: &domain.ProductPayload{
				CatalogID:         "cat_explicit_123",
				ProductRetailerID: "sku_abc",
			},
		}

		qMsg, err := p.Ingest(context.Background(), wsID, traceID, req)
		if err != nil {
			t.Fatalf("Ingest failed: %v", err)
		}
		if qMsg.Product == nil || qMsg.Product.CatalogID != "cat_explicit_123" {
			t.Errorf("got catalog_id %v, want cat_explicit_123", qMsg.Product)
		}
	})

	t.Run("Fallback to default_catalog_id from connection credentials", func(t *testing.T) {
		creds, _ := json.Marshal(map[string]string{
			"default_catalog_id": "cat_default_456",
		})
		conn := &repository.Connection{
			ID:             connID,
			WorkspaceID:    wsID,
			Channel:        "whatsapp_cloud",
			SenderIdentity: "12345",
			Status:         "connected",
			Credentials:    creds,
		}
		p := outbound.NewProcessor(nil, nil, &fakeRouteResolver{conn: conn}, &fakePublisher{})

		req := &domain.CreateMessageRequest{
			To:      "5511999999999",
			Channel: "whatsapp_cloud",
			Type:    "product",
			Product: &domain.ProductPayload{
				ProductRetailerID: "sku_abc",
			},
		}

		qMsg, err := p.Ingest(context.Background(), wsID, traceID, req)
		if err != nil {
			t.Fatalf("Ingest failed: %v", err)
		}
		if qMsg.Product == nil || qMsg.Product.CatalogID != "cat_default_456" {
			t.Errorf("got catalog_id %v, want cat_default_456", qMsg.Product)
		}
	})

	t.Run("Missing catalog_id in both request and connection returns ErrMissingCatalogID", func(t *testing.T) {
		conn := &repository.Connection{
			ID:             connID,
			WorkspaceID:    wsID,
			Channel:        "whatsapp_cloud",
			SenderIdentity: "12345",
			Status:         "connected",
		}
		p := outbound.NewProcessor(nil, nil, &fakeRouteResolver{conn: conn}, nil)

		req := &domain.CreateMessageRequest{
			To:      "5511999999999",
			Channel: "whatsapp_cloud",
			Type:    "product",
			Product: &domain.ProductPayload{
				ProductRetailerID: "sku_abc",
			},
		}

		_, err := p.Ingest(context.Background(), wsID, traceID, req)
		if !errors.Is(err, outbound.ErrMissingCatalogID) {
			t.Fatalf("got error %v, want ErrMissingCatalogID", err)
		}
	})

	t.Run("Bounds violation returns ErrInvalidProductPayload", func(t *testing.T) {
		conn := &repository.Connection{
			ID:             connID,
			WorkspaceID:    wsID,
			Channel:        "whatsapp_cloud",
			SenderIdentity: "12345",
			Status:         "connected",
		}
		p := outbound.NewProcessor(nil, nil, &fakeRouteResolver{conn: conn}, nil)

		req := &domain.CreateMessageRequest{
			To:      "5511999999999",
			Channel: "whatsapp_cloud",
			Type:    "product_list",
			Product: &domain.ProductPayload{
				CatalogID: "cat_123",
				Sections: []domain.ProductSection{
					{
						Title: "This section title is way too long and exceeds 24 characters limit",
						ProductItems: []domain.ProductItem{
							{ProductRetailerID: "sku_1"},
						},
					},
				},
			},
		}

		_, err := p.Ingest(context.Background(), wsID, traceID, req)
		if !errors.Is(err, outbound.ErrInvalidProductPayload) {
			t.Fatalf("got error %v, want ErrInvalidProductPayload", err)
		}
	})
}

func TestProcessor_WhatsAppCloudWindowIngestion(t *testing.T) {
	wsID := uuid.New()
	connID := uuid.New()
	senderIdentity := "+5511888880000"
	contactPhone := "5511999990000"

	wabaConn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		Name:           "WABA Active Conn",
		Channel:        "whatsapp_cloud",
		SenderIdentity: senderIdentity,
		Status:         "connected",
	}

	resolver := &fakeRouteResolver{conn: wabaConn}
	publisher := &fakePublisher{}

	t.Run("freeform message passes within 24h standard session", func(t *testing.T) {
		sessionReader := &fakeSessionReader{
			sess: &repository.RecipientSession{
				SessionKey: domain.SessionKey{
					WorkspaceID:       wsID,
					RecipientPhone:    contactPhone,
					Channel:           "whatsapp_cloud",
					RecipientIdentity: senderIdentity,
				},
				LastInboundAt:  time.Now().Add(-2 * time.Hour),
				EntryPointType: "standard",
			},
		}
		p := outbound.NewProcessor(nil, nil, resolver, publisher)
		p.SetWindowChecker(session.NewWindowChecker(sessionReader))

		req := &domain.CreateMessageRequest{
			To:      contactPhone,
			Channel: "whatsapp_cloud",
			Body:    "Freeform reply within 24h window",
		}

		qMsg, err := p.Ingest(context.Background(), wsID, "trace-window-1", req)
		if err != nil {
			t.Fatalf("expected freeform message to pass ingestion within 24h, got: %v", err)
		}
		if qMsg.Body != "Freeform reply within 24h window" {
			t.Errorf("expected Body to be preserved, got %q", qMsg.Body)
		}
		if qMsg.TemplateName != "" {
			t.Errorf("expected no template conversion, got template %q", qMsg.TemplateName)
		}
	})

	t.Run("freeform message passes within 72h CTWA session at 40h", func(t *testing.T) {
		sessionReader := &fakeSessionReader{
			sess: &repository.RecipientSession{
				SessionKey: domain.SessionKey{
					WorkspaceID:       wsID,
					RecipientPhone:    contactPhone,
					Channel:           "whatsapp_cloud",
					RecipientIdentity: senderIdentity,
				},
				LastInboundAt:  time.Now().Add(-40 * time.Hour),
				EntryPointType: "ctwa",
			},
		}
		p := outbound.NewProcessor(nil, nil, resolver, publisher)
		p.SetWindowChecker(session.NewWindowChecker(sessionReader))

		req := &domain.CreateMessageRequest{
			To:      contactPhone,
			Channel: "whatsapp_cloud",
			Body:    "Freeform reply within 72h CTWA window",
		}

		qMsg, err := p.Ingest(context.Background(), wsID, "trace-window-2", req)
		if err != nil {
			t.Fatalf("expected freeform message to pass ingestion within 72h CTWA, got: %v", err)
		}
		if qMsg.Body != "Freeform reply within 72h CTWA window" {
			t.Errorf("expected Body to be preserved, got %q", qMsg.Body)
		}
	})

	t.Run("freeform message fails with SessionWindowError when window expired and no fallback template", func(t *testing.T) {
		sessionReader := &fakeSessionReader{
			sess: &repository.RecipientSession{
				SessionKey: domain.SessionKey{
					WorkspaceID:       wsID,
					RecipientPhone:    contactPhone,
					Channel:           "whatsapp_cloud",
					RecipientIdentity: senderIdentity,
				},
				LastInboundAt:  time.Now().Add(-26 * time.Hour),
				EntryPointType: "standard",
			},
		}
		p := outbound.NewProcessor(nil, nil, resolver, publisher)
		p.SetWindowChecker(session.NewWindowChecker(sessionReader))

		req := &domain.CreateMessageRequest{
			To:      contactPhone,
			Channel: "whatsapp_cloud",
			Body:    "Freeform reply outside window",
		}

		_, err := p.Ingest(context.Background(), wsID, "trace-window-3", req)
		if err == nil {
			t.Fatalf("expected SessionWindowError for expired window, got nil")
		}

		var sessErr *session.SessionWindowError
		if !errors.As(err, &sessErr) {
			t.Fatalf("expected error to be *session.SessionWindowError, got %T: %v", err, err)
		}
	})

	t.Run("WABA freeform falls back to default template when window expired", func(t *testing.T) {
		creds, _ := json.Marshal(map[string]string{
			"default_template_name":     "reengagement_prompt",
			"default_template_language": "pt_BR",
		})
		fallbackConn := &repository.Connection{
			ID:             connID,
			WorkspaceID:    wsID,
			Name:           "WABA Fallback Conn",
			Channel:        "whatsapp_cloud",
			SenderIdentity: senderIdentity,
			Credentials:    creds,
			Status:         "connected",
		}
		sessionReader := &fakeSessionReader{
			sess: &repository.RecipientSession{
				SessionKey: domain.SessionKey{
					WorkspaceID:       wsID,
					RecipientPhone:    contactPhone,
					Channel:           "whatsapp_cloud",
					RecipientIdentity: senderIdentity,
				},
				LastInboundAt:  time.Now().Add(-30 * time.Hour),
				EntryPointType: "standard",
			},
		}
		p := outbound.NewProcessor(nil, nil, &fakeRouteResolver{conn: fallbackConn}, publisher)
		p.SetWindowChecker(session.NewWindowChecker(sessionReader))

		req := &domain.CreateMessageRequest{
			To:      contactPhone,
			Channel: "whatsapp_cloud",
			Body:    "Message converted to template",
		}

		qMsg, err := p.Ingest(context.Background(), wsID, "trace-window-fallback", req)
		if err != nil {
			t.Fatalf("expected successful ingestion with template fallback, got: %v", err)
		}
		if qMsg.TemplateName != "reengagement_prompt" {
			t.Errorf("got TemplateName %q, want 'reengagement_prompt'", qMsg.TemplateName)
		}
		if qMsg.Language != "pt_BR" {
			t.Errorf("got Language %q, want 'pt_BR'", qMsg.Language)
		}
		if len(qMsg.Components) != 1 {
			t.Fatalf("expected 1 body component, got: %+v", qMsg.Components)
		}
		params, ok := qMsg.Components[0].Parameters.([]domain.TemplateParameter)
		if !ok || len(params) != 1 {
			t.Fatalf("expected 1 parameter of type []domain.TemplateParameter, got: %+v", qMsg.Components[0].Parameters)
		}
		if params[0].Text != "Message converted to template" {
			t.Errorf("got parameter text %q, want %q", params[0].Text, "Message converted to template")
		}
	})

	t.Run("WhatsApp Web (whatsmeow) bypasses session window check completely", func(t *testing.T) {
		waWebConn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    wsID,
			Name:           "WA Web Conn",
			Channel:        "whatsapp",
			SenderIdentity: "+5511777770000",
			Status:         "connected",
		}
		sessionReader := &fakeSessionReader{
			sess: nil, // no session recorded
		}
		p := outbound.NewProcessor(nil, nil, &fakeRouteResolver{conn: waWebConn}, publisher)
		p.SetWindowChecker(session.NewWindowChecker(sessionReader))

		req := &domain.CreateMessageRequest{
			To:      contactPhone,
			Channel: "whatsapp",
			Body:    "WhatsApp Web message with no window check",
		}

		qMsg, err := p.Ingest(context.Background(), wsID, "trace-wa-web-bypass", req)
		if err != nil {
			t.Fatalf("expected WhatsApp Web to bypass window check, got error: %v", err)
		}
		if qMsg.Body != "WhatsApp Web message with no window check" {
			t.Errorf("expected Body preserved, got %q", qMsg.Body)
		}
	})

	t.Run("Telegram bypasses session window check completely", func(t *testing.T) {
		telegramConn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    wsID,
			Name:           "Telegram Conn",
			Channel:        "telegram",
			SenderIdentity: "@my_bot",
			Status:         "connected",
		}
		sessionReader := &fakeSessionReader{
			sess: nil, // no session recorded
		}
		p := outbound.NewProcessor(nil, nil, &fakeRouteResolver{conn: telegramConn}, publisher)
		p.SetWindowChecker(session.NewWindowChecker(sessionReader))

		req := &domain.CreateMessageRequest{
			To:      "123456789",
			Channel: "telegram",
			Body:    "Telegram message with no window check",
		}

		qMsg, err := p.Ingest(context.Background(), wsID, "trace-telegram-bypass", req)
		if err != nil {
			t.Fatalf("expected Telegram to bypass window check, got error: %v", err)
		}
		if qMsg.Body != "Telegram message with no window check" {
			t.Errorf("expected Body preserved, got %q", qMsg.Body)
		}
	})
}
