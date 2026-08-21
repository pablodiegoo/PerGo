package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://admin:admin@localhost:5432/pergo?sslmode=disable"
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

func TestInstagramDispatch(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	kek := make([]byte, 32)
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connectionsRepo := repository.NewConnectionRepository(pool, enc)
	wsRepo := repository.NewWorkspaceRepository(pool)

	ws, err := wsRepo.Create(ctx, "ig_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	igConfig := InstagramConfig{
		InstagramAccountID: "ig_account_123",
		Token:              "ig_test_token",
	}
	configBytes, _ := json.Marshal(igConfig)
	connID := uuid.New()
	err = connectionsRepo.Create(ctx, &repository.Connection{
		ID:             connID,
		WorkspaceID:    ws.ID,
		Name:           "Instagram",
		Channel:        "instagram",
		SenderIdentity: "ig_account_123",
		Status:         "active",
		Credentials:    configBytes,
	})
	if err != nil {
		t.Fatalf("failed to save Instagram credentials: %v", err)
	}

	tenantCtx := tenant.WithWorkspaceID(context.Background(), ws.ID)

	t.Run("Success Send Interactive Native Buttons", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			var req instagramMessageRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}

			if req.Message.Interactive == nil || req.Message.Interactive.Type != "button" {
				t.Errorf("expected interactive button, got %+v", req.Message.Interactive)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message_id":"ig_msg_123"}`))
		}))
		defer server.Close()

		adapter := NewAdapter(connectionsRepo, server.Client(), "")
		adapter.baseURL = server.URL

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "ig_account_123",
			To:             "recipient_ig_id",
			Interactive: &domain.Interactive{
				Type: "button",
				Body: domain.TextContent{Text: "Choose an option"},
				Action: domain.Action{
					Buttons: []domain.Button{
						{Type: "reply", Reply: domain.Reply{ID: "1", Title: "Option 1"}},
						{Type: "reply", Reply: domain.Reply{ID: "2", Title: "Option 2"}},
					},
				},
			},
		}

		resp, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		if resp != "ig_msg_123" {
			t.Errorf("expected msg id ig_msg_123, got: %s", resp)
		}
	})

	t.Run("Instagram Interactive Button Degrade (>3 buttons)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			var req instagramMessageRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}

			if req.Message.Text == "" || req.Message.Interactive != nil {
				t.Errorf("expected degraded text message, got text: %q, inter: %+v", req.Message.Text, req.Message.Interactive)
			}
			if !strings.Contains(req.Message.Text, "4. Option 4") {
				t.Errorf("expected text to contain option 4, got: %s", req.Message.Text)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message_id":"ig_degraded_btn"}`))
		}))
		defer server.Close()

		adapter := NewAdapter(connectionsRepo, server.Client(), "")
		adapter.baseURL = server.URL

		payload := &channel.MessagePayload{
			ConnectionID:     connID,
			SenderIdentity:   "ig_account_123",
			To:               "recipient_ig_id",
			FallbackBehavior: "degrade",
			Interactive: &domain.Interactive{
				Type: "button",
				Body: domain.TextContent{Text: "Choose an option"},
				Action: domain.Action{
					Buttons: []domain.Button{
						{Type: "reply", Reply: domain.Reply{ID: "1", Title: "Option 1"}},
						{Type: "reply", Reply: domain.Reply{ID: "2", Title: "Option 2"}},
						{Type: "reply", Reply: domain.Reply{ID: "3", Title: "Option 3"}},
						{Type: "reply", Reply: domain.Reply{ID: "4", Title: "Option 4"}},
					},
				},
			},
		}

		resp, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on degrade, got: %v", err)
		}
		if resp != "ig_degraded_btn" {
			t.Errorf("expected msg id ig_degraded_btn, got: %s", resp)
		}
	})

	t.Run("Instagram Interactive Button Fail (>3 buttons & fallback_behavior fail)", func(t *testing.T) {
		adapter := NewAdapter(connectionsRepo, nil, "")

		payload := &channel.MessagePayload{
			ConnectionID:     connID,
			SenderIdentity:   "ig_account_123",
			To:               "recipient_ig_id",
			FallbackBehavior: "fail",
			Interactive: &domain.Interactive{
				Type: "button",
				Body: domain.TextContent{Text: "Choose an option"},
				Action: domain.Action{
					Buttons: []domain.Button{
						{Type: "reply", Reply: domain.Reply{ID: "1", Title: "Option 1"}},
						{Type: "reply", Reply: domain.Reply{ID: "2", Title: "Option 2"}},
						{Type: "reply", Reply: domain.Reply{ID: "3", Title: "Option 3"}},
						{Type: "reply", Reply: domain.Reply{ID: "4", Title: "Option 4"}},
					},
				},
			},
		}

		_, err := adapter.Dispatch(tenantCtx, payload)
		if err == nil {
			t.Fatal("expected error on fallback fail, got nil")
		}
		if !channel.IsTerminal(err) {
			t.Errorf("expected terminal error, got: %v", err)
		}
	})

	t.Run("Instagram Interactive List Degrade (>10 rows)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			var req instagramMessageRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}

			if req.Message.Text == "" || req.Message.Interactive != nil {
				t.Errorf("expected degraded text message, got: %+v", req.Message)
			}
			if !strings.Contains(req.Message.Text, "11. Item 11") {
				t.Errorf("expected text to contain Item 11, got: %s", req.Message.Text)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message_id":"ig_degraded_list"}`))
		}))
		defer server.Close()

		adapter := NewAdapter(connectionsRepo, server.Client(), "")
		adapter.baseURL = server.URL

		var rows []domain.Row
		for i := 1; i <= 11; i++ {
			rows = append(rows, domain.Row{
				ID:    fmt.Sprintf("r_%d", i),
				Title: fmt.Sprintf("Item %d", i),
			})
		}

		payload := &channel.MessagePayload{
			ConnectionID:     connID,
			SenderIdentity:   "ig_account_123",
			To:               "recipient_ig_id",
			FallbackBehavior: "degrade",
			Interactive: &domain.Interactive{
				Type: "list",
				Body: domain.TextContent{Text: "Choose an item"},
				Action: domain.Action{
					Button: "Options",
					Sections: []domain.Section{
						{
							Title: "Section 1",
							Rows:  rows,
						},
					},
				},
			},
		}

		resp, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on degrade, got: %v", err)
		}
		if resp != "ig_degraded_list" {
			t.Errorf("expected msg id ig_degraded_list, got: %s", resp)
		}
	})

	t.Run("Instagram Interactive List Fail (>10 rows & fallback_behavior fail)", func(t *testing.T) {
		adapter := NewAdapter(connectionsRepo, nil, "")

		var rows []domain.Row
		for i := 1; i <= 11; i++ {
			rows = append(rows, domain.Row{
				ID:    fmt.Sprintf("r_%d", i),
				Title: fmt.Sprintf("Item %d", i),
			})
		}

		payload := &channel.MessagePayload{
			ConnectionID:     connID,
			SenderIdentity:   "ig_account_123",
			To:               "recipient_ig_id",
			FallbackBehavior: "fail",
			Interactive: &domain.Interactive{
				Type: "list",
				Body: domain.TextContent{Text: "Choose an item"},
				Action: domain.Action{
					Button: "Options",
					Sections: []domain.Section{
						{
							Title: "Section 1",
							Rows:  rows,
						},
					},
				},
			},
		}

		_, err := adapter.Dispatch(tenantCtx, payload)
		if err == nil {
			t.Fatal("expected error on fallback fail for list, got nil")
		}
		if !channel.IsTerminal(err) {
			t.Errorf("expected terminal error, got: %v", err)
		}
	})

	t.Run("Instagram Interactive Flow Degrade", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			var req instagramMessageRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}

			if !strings.Contains(req.Message.Text, "Survey") {
				t.Errorf("expected degraded text with survey, got: %s", req.Message.Text)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message_id":"ig_degraded_flow"}`))
		}))
		defer server.Close()

		adapter := NewAdapter(connectionsRepo, server.Client(), "")
		adapter.baseURL = server.URL

		payload := &channel.MessagePayload{
			ConnectionID:     connID,
			SenderIdentity:   "ig_account_123",
			To:               "recipient_ig_id",
			FallbackBehavior: "degrade",
			Interactive: &domain.Interactive{
				Type: "flow",
				Body: domain.TextContent{Text: "Fill out Survey"},
				Action: domain.Action{
					FlowCTA: "Start",
				},
			},
		}

		resp, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on degrade flow, got: %v", err)
		}
		if resp != "ig_degraded_flow" {
			t.Errorf("expected msg id ig_degraded_flow, got: %s", resp)
		}
	})

	t.Run("Instagram Interactive Flow Fail on fallback_behavior fail", func(t *testing.T) {
		adapter := NewAdapter(connectionsRepo, nil, "")

		payload := &channel.MessagePayload{
			ConnectionID:     connID,
			SenderIdentity:   "ig_account_123",
			To:               "recipient_ig_id",
			FallbackBehavior: "fail",
			Interactive: &domain.Interactive{
				Type: "flow",
				Body: domain.TextContent{Text: "Fill out Survey"},
				Action: domain.Action{
					FlowCTA: "Start",
				},
			},
		}

		_, err := adapter.Dispatch(tenantCtx, payload)
		if err == nil {
			t.Fatal("expected error on fallback fail for flow, got nil")
		}
		if !channel.IsTerminal(err) {
			t.Errorf("expected terminal error, got: %v", err)
		}
	})
}
