package inbound_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

// getTestPool connects to a local test PostgreSQL database.
func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available at %s: %v", dsn, err)
	}

	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed at %s: %v", dsn, err)
	}

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to wrap pool as sql.DB: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		pool.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	return pool
}

// fakePublisher records published messages.
type fakePublisher struct {
	published []struct {
		subject string
		data    []byte
		traceID string
	}
	err error
}

func (f *fakePublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, struct {
		subject string
		data    []byte
		traceID string
	}{subject, data, traceID})
	return nil
}

// fakeMediaEngine records uploads and processes.
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

// fakeAuditWriter records audit events.
type fakeAuditWriter struct {
	events []audit.Event
	err    error
}

func (f *fakeAuditWriter) Write(event audit.Event) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, event)
	return nil
}

func (f *fakeAuditWriter) Close() error {
	return nil
}

func TestInboundProcessor_Process(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	// Create test workspace
	wsName := "inbound_proc_test_" + uuid.New().String()
	ws, err := wsRepo.Create(ctx, wsName)
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	t.Run("Standard text message ingestion", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo)

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-msg-123",
			Channel:     "telegram",
			From:        "user-chat-99",
			To:          "@test_bot",
			Body:        "Hello PerGo!",
		}

		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		// Verify NATS Publish
		if len(pub.published) != 1 {
			t.Fatalf("expected 1 published event, got %d", len(pub.published))
		}
		pubEvent := pub.published[0]
		if pubEvent.subject != "inbound.events."+ws.ID.String() {
			t.Errorf("got NATS subject %s, want inbound.events.%s", pubEvent.subject, ws.ID.String())
		}

		var payload inbound.InboundEventPayload
		if err := json.Unmarshal(pubEvent.data, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		if payload.Body != "Hello PerGo!" {
			t.Errorf("got payload body %q, want %q", payload.Body, "Hello PerGo!")
		}
		if payload.Channel != "telegram" {
			t.Errorf("got payload channel %q, want %q", payload.Channel, "telegram")
		}

		// Verify Recipient Session
		sess, err := sessRepo.Get(ctx, ws.ID, "user-chat-99", "telegram", "@test_bot")
		if err != nil {
			t.Fatalf("failed to get recipient session: %v", err)
		}
		if sess.RecipientPhone != "user-chat-99" {
			t.Errorf("got session recipient %q, want %q", sess.RecipientPhone, "user-chat-99")
		}
	})

	t.Run("Deduplication prevents double processing", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo)

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-msg-dup",
			Channel:     "whatsapp",
			From:        "5511999990000",
			To:          "5511888880000",
			Body:        "First attempt",
		}

		// 1st run: unique
		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed on first run: %v", err)
		}

		// 2nd run: duplicate
		event.Body = "Second attempt"
		err = proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed on second run: %v", err)
		}

		// Verify only 1 message was published
		if len(pub.published) != 1 {
			t.Errorf("expected 1 published event, got %d", len(pub.published))
		}
	})

	t.Run("Media upload and mapping", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{
			processInboundFn: func(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error) {
				return "/media/" + workspaceID.String() + "/abcde12345.jpg", nil
			},
		}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo)

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-media-1",
			Channel:     "whatsapp_cloud",
			From:        "sender-123",
			To:          "receiver-456",
			Media: &inbound.InboundMedia{
				Bytes:     []byte("fake-image-bytes"),
				MediaType: "image",
				Filename:  "picture.jpg",
				Caption:   "Beautiful sunset",
			},
		}

		err := proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		// Verify Media Engine Inbound Call
		if len(me.inboundCalls) != 1 {
			t.Fatalf("expected 1 ProcessInbound call, got %d", len(me.inboundCalls))
		}
		call := me.inboundCalls[0]
		if call.mediaType != "image" {
			t.Errorf("got mediaType %s, want image", call.mediaType)
		}
		if string(call.data) != "fake-image-bytes" {
			t.Errorf("got data %s, want fake-image-bytes", string(call.data))
		}

		// Verify Media URL in payload
		var payload inbound.InboundEventPayload
		json.Unmarshal(pub.published[0].data, &payload)
		if payload.Media == nil {
			t.Fatal("expected media in payload")
		}
		if payload.Media.MediaType != "image" {
			t.Errorf("got media type %s, want image", payload.Media.MediaType)
		}
		if !strings.HasPrefix(payload.Media.MediaURL, "/media/"+ws.ID.String()+"/") {
			t.Errorf("invalid media URL format: %s", payload.Media.MediaURL)
		}
	})

	t.Run("PII Opt-in opt-out filters", func(t *testing.T) {
		// Update workspace PIIOptIn to false
		_, err = pool.Exec(ctx, "UPDATE workspaces SET pii_opt_in = FALSE WHERE id = $1", ws.ID)
		if err != nil {
			t.Fatalf("failed to update workspace PII: %v", err)
		}

		pub := &fakePublisher{}
		me := &fakeMediaEngine{}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo)

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-pii-opt-out",
			Channel:     "telegram",
			From:        "sender-pii",
			To:          "receiver-pii",
			Body:        "PII Message",
			Location: &inbound.InboundLocation{
				Latitude:  12.34,
				Longitude: 56.78,
				Name:      "Secret Base",
			},
			Contacts: []inbound.InboundContact{
				{Name: "John Doe", Phone: "+1234"},
			},
		}

		err = proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		var payload inbound.InboundEventPayload
		json.Unmarshal(pub.published[0].data, &payload)

		// Verification: Location and Contacts should be filtered (nil/empty)
		if payload.Location != nil {
			t.Errorf("expected Location to be filtered (nil), got %+v", payload.Location)
		}
		if len(payload.Contacts) != 0 {
			t.Errorf("expected Contacts to be filtered (empty), got %+v", payload.Contacts)
		}

		// Update workspace PIIOptIn to true
		_, err = pool.Exec(ctx, "UPDATE workspaces SET pii_opt_in = TRUE WHERE id = $1", ws.ID)
		if err != nil {
			t.Fatalf("failed to update workspace PII: %v", err)
		}

		pub2 := &fakePublisher{}
		proc2 := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub2, aud, sessRepo, contactRepo, dispatchRepo)
		event.MessageID = "test-pii-opt-in" // fresh message ID to bypass dedup

		err = proc2.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		var payload2 inbound.InboundEventPayload
		json.Unmarshal(pub2.published[0].data, &payload2)

		// Verification: Location and Contacts should be retained
		if payload2.Location == nil {
			t.Fatal("expected Location to be retained")
		}
		if payload2.Location.Latitude != 12.34 {
			t.Errorf("got latitude %f, want 12.34", payload2.Location.Latitude)
		}
		if len(payload2.Contacts) != 1 {
			t.Fatalf("expected 1 contact, got %d", len(payload2.Contacts))
		}
	})

	t.Run("Non-fatal S3 upload failure does not stop message ingestion", func(t *testing.T) {
		pub := &fakePublisher{}
		me := &fakeMediaEngine{
			processInboundFn: func(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error) {
				return "", errors.New("S3 server timeout")
			},
		}
		aud := &fakeAuditWriter{}

		proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo)

		event := &inbound.InboundEvent{
			WorkspaceID: ws.ID,
			MessageID:   "test-s3-fail",
			Channel:     "whatsapp",
			From:        "sender-s3",
			To:          "receiver-s3",
			Body:        "Text still goes through",
			Media: &inbound.InboundMedia{
				Bytes:     []byte("some-bytes"),
				MediaType: "document",
			},
		}

		err = proc.Process(ctx, event)
		if err != nil {
			t.Fatalf("Process should not fail: %v", err)
		}

		// Verify message was still published to NATS
		if len(pub.published) != 1 {
			t.Fatalf("expected 1 published event, got %d", len(pub.published))
		}
		var payload inbound.InboundEventPayload
		json.Unmarshal(pub.published[0].data, &payload)

		if payload.Body != "Text still goes through" {
			t.Errorf("got body %q, want %q", payload.Body, "Text still goes through")
		}
		// Media should be nil since S3 upload failed
		if payload.Media != nil {
			t.Errorf("expected media to be nil on S3 fail, got %+v", payload.Media)
		}
	})
}

func TestProcess_StatusUpdate(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "status_update_test_ws_" + uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	traceID := uuid.New().String()
	d, err := dispatchRepo.GetOrCreateDispatch(ctx, ws.ID, traceID, "whatsapp_cloud", nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create dispatch: %v", err)
	}

	providerID := "wamid.test_status_update_999"
	err = dispatchRepo.UpdateProviderMessageID(ctx, d.ID, providerID)
	if err != nil {
		t.Fatalf("failed to update provider message id: %v", err)
	}

	// Initial dispatch status should be "queued"
	if d.Status != "queued" {
		t.Errorf("expected initial status queued, got %s", d.Status)
	}

	// Count initial contacts
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM contacts WHERE workspace_id = $1", ws.ID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count contacts: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 contacts initially, got %d", count)
	}

	pub := &fakePublisher{}
	me := &fakeMediaEngine{}
	aud := &fakeAuditWriter{}

	proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, me, pub, aud, sessRepo, contactRepo, dispatchRepo)

	event := &inbound.InboundEvent{
		WorkspaceID: ws.ID,
		MessageID:   providerID,
		Channel:     "whatsapp_cloud",
		From:        "5511999990000",
		To:          "5511888880000",
		Body:        "delivered",
		Metadata:    map[string]string{"type": "status_update"},
	}

	err = proc.Process(ctx, event)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// 1. Verify dispatch record status is updated to "delivered"
	updated, err := dispatchRepo.GetByProviderMessageID(ctx, providerID)
	if err != nil {
		t.Fatalf("failed to retrieve updated dispatch: %v", err)
	}
	if updated.Status != "delivered" {
		t.Errorf("expected status 'delivered', got %q", updated.Status)
	}

	// 2. Verify contact resolution was not executed (0 contacts in DB)
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM contacts WHERE workspace_id = $1", ws.ID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count contacts: %v", err)
	}
	if count != 0 {
		t.Errorf("expected contact resolution to be bypassed, but contact was created (count = %d)", count)
	}

	// 3. Verify NATS publish on messages.status_updated
	if len(pub.published) != 1 {
		t.Fatalf("expected 1 NATS publish, got %d", len(pub.published))
	}
	pubEvent := pub.published[0]
	if pubEvent.subject != "messages.status_updated" {
		t.Errorf("expected subject 'messages.status_updated', got %q", pubEvent.subject)
	}

	var payload inbound.MessageStatusUpdatedPayload
	if err := json.Unmarshal(pubEvent.data, &payload); err != nil {
		t.Fatalf("failed to unmarshal NATS payload: %v", err)
	}
	if payload.WorkspaceID != ws.ID.String() {
		t.Errorf("expected workspace ID %s, got %s", ws.ID.String(), payload.WorkspaceID)
	}
	if payload.DispatchID != d.ID.String() {
		t.Errorf("expected dispatch ID %s, got %s", d.ID.String(), payload.DispatchID)
	}
	if payload.MessageID != providerID {
		t.Errorf("expected message ID %s, got %s", providerID, payload.MessageID)
	}
	if payload.Status != "delivered" {
		t.Errorf("expected status 'delivered', got %s", payload.Status)
	}
}
