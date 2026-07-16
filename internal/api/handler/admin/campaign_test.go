package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
)

func connectNATS(t *testing.T) *nats.Conn {
	t.Helper()
	url := os.Getenv("PERGO_NATS_URL")
	if url == "" {
		url = nats.DefaultURL
	}
	nc, err := nats.Connect(url)
	if err != nil {
		t.Skipf("NATS not available at %s: %v", url, err)
	}
	t.Cleanup(func() {
		nc.Close()
	})
	return nc
}

func TestCampaignHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	nc := connectNATS(t)
	pub := queue.NewJetStreamPublisher(nc)

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campaignRepo := repository.NewCampaignRepository(pool)
	templateRepo := repository.NewWABATemplateRepository(pool)

	kek := make([]byte, 32)
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connectionRepo := repository.NewConnectionRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "camp_handler_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// Create default connection
	err = connectionRepo.Create(ctx, &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WhatsApp Web",
		Channel:        "whatsapp",
		SenderIdentity: "5511999990002",
		Status:         "connected",
		IsDefault:      true,
	})
	if err != nil {
		t.Fatalf("failed to create default connection: %v", err)
	}

	// Ensure stream exists
	_, err = queue.EnsureCampaignStream(ctx, nc)
	if err != nil {
		t.Fatalf("EnsureCampaignStream failed: %v", err)
	}

	h := admin.NewCampaignHandler(campaignRepo, templateRepo, connectionRepo, pub)
	e := echo.New()

	t.Run("NewForm", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/campaigns/new", ws.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/campaigns/new")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		if err := h.NewForm(c); err != nil {
			t.Fatalf("NewForm failed: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Nova Campanha") {
			t.Errorf("expected form title in response, got: %s", rec.Body.String())
		}
	})

	t.Run("Upload CSV Preview", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("csv_file", "contacts.csv")

		csvContent := "phone,name\n5511999998888,John\n5511999998888,John\ninvalid-phone,Bad\n5511988887777,Alice\n"
		_, _ = part.Write([]byte(csvContent))
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/campaigns/upload", ws.ID), body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/campaigns/upload")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		if err := h.UploadCSV(c); err != nil {
			t.Fatalf("UploadCSV failed: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
		bodyStr := rec.Body.String()
		if !strings.Contains(bodyStr, "Resultado da Validação") {
			t.Errorf("expected preview segment in response, got: %s", bodyStr)
		}
		if !strings.Contains(bodyStr, "5511999998888") {
			t.Errorf("expected sanitized E.164 phone in preview, got: %s", bodyStr)
		}
	})

	t.Run("Create Campaign", func(t *testing.T) {
		recipients := []domain.CampaignRecipient{
			{To: "5511999998888", Variables: map[string]string{"name": "John"}},
			{To: "5511988887777", Variables: map[string]string{"name": "Alice"}},
		}
		recipientsJSON, _ := json.Marshal(recipients)

		skipped := []domain.SkippedRow{
			{LineNumber: 3, RawInput: "invalid-phone,Bad", Reason: "numero de telefone invalido (tamanho 13)"},
		}
		skippedJSON, _ := json.Marshal(skipped)

		form := url.Values{}
		form.Set("name", "Campanha Vendas Julho")
		form.Set("channel", "whatsapp")
		form.Set("batch_size", "50")
		form.Set("delay_seconds", "3")
		form.Set("body_template", "Ola {{name}}!")
		form.Set("recipients_data", string(recipientsJSON))
		form.Set("skipped_data", string(skippedJSON))

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/campaigns", ws.ID), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/campaigns")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		if err := h.Create(c); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		camps, err := campaignRepo.ListByWorkspace(ctx, ws.ID)
		if err != nil {
			t.Fatalf("ListByWorkspace failed: %v", err)
		}
		if len(camps) != 1 {
			t.Fatalf("expected 1 campaign in DB, got %d", len(camps))
		}
		if camps[0].Name != "Campanha Vendas Julho" {
			t.Errorf("expected campaign name 'Campanha Vendas Julho', got '%s'", camps[0].Name)
		}
		if len(camps[0].Recipients) != 2 {
			t.Errorf("expected 2 recipients in DB, got %d", len(camps[0].Recipients))
		}
		if len(camps[0].SkippedRows) != 1 {
			t.Errorf("expected 1 skipped row in DB, got %d", len(camps[0].SkippedRows))
		}

		campaignID := camps[0].ID

		// Test List
		reqList := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/campaigns", ws.ID), nil)
		recList := httptest.NewRecorder()
		cList := e.NewContext(reqList, recList)
		cList.SetPath("/admin/workspaces/:workspace_id/campaigns")
		cList.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})
		if err := h.List(cList); err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if recList.Code != http.StatusOK {
			t.Errorf("List status expected 200, got %d", recList.Code)
		}
		if !strings.Contains(recList.Body.String(), "Campanha Vendas Julho") {
			t.Errorf("List body expected campaign name, got: %s", recList.Body.String())
		}

		// Test Download Skipped
		reqDownload := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/campaigns/%s/skipped/download", ws.ID, campaignID), nil)
		recDownload := httptest.NewRecorder()
		cDownload := e.NewContext(reqDownload, recDownload)
		cDownload.SetPath("/admin/workspaces/:workspace_id/campaigns/:id/skipped/download")
		cDownload.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
			{Name: "id", Value: campaignID.String()},
		})
		if err := h.DownloadSkipped(cDownload); err != nil {
			t.Fatalf("DownloadSkipped failed: %v", err)
		}
		if recDownload.Code != http.StatusOK {
			t.Errorf("DownloadSkipped status expected 200, got %d", recDownload.Code)
		}
		if !strings.Contains(recDownload.Body.String(), "invalid-phone,Bad") {
			t.Errorf("DownloadSkipped CSV body expected skipped row raw input, got: %s", recDownload.Body.String())
		}

		// Test Start
		reqStart := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/campaigns/%s/start", ws.ID, campaignID), nil)
		recStart := httptest.NewRecorder()
		cStart := e.NewContext(reqStart, recStart)
		cStart.SetPath("/admin/workspaces/:workspace_id/campaigns/:id/start")
		cStart.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
			{Name: "id", Value: campaignID.String()},
		})
		if err := h.Start(cStart); err != nil {
			t.Fatalf("Start failed: %v", err)
		}
		if recStart.Code != http.StatusOK {
			t.Errorf("Start status expected 200, got %d", recStart.Code)
		}
		if !strings.Contains(recStart.Body.String(), "Enviando") {
			t.Errorf("Start response expected status 'Enviando', got: %s", recStart.Body.String())
		}

		// Verify status updated in DB
		updatedCamp, _ := campaignRepo.GetByID(ctx, campaignID)
		if updatedCamp.Status != domain.CampaignStatusSending {
			t.Errorf("expected DB status to be 'sending', got '%s'", updatedCamp.Status)
		}

		// Test Cancel
		reqCancel := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/campaigns/%s/cancel", ws.ID, campaignID), nil)
		recCancel := httptest.NewRecorder()
		cCancel := e.NewContext(reqCancel, recCancel)
		cCancel.SetPath("/admin/workspaces/:workspace_id/campaigns/:id/cancel")
		cCancel.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
			{Name: "id", Value: campaignID.String()},
		})
		if err := h.Cancel(cCancel); err != nil {
			t.Fatalf("Cancel failed: %v", err)
		}
		if recCancel.Code != http.StatusOK {
			t.Errorf("Cancel status expected 200, got %d", recCancel.Code)
		}
		if !strings.Contains(recCancel.Body.String(), "Cancelada") {
			t.Errorf("Cancel response expected status 'Cancelada', got: %s", recCancel.Body.String())
		}

		cancelledCamp, _ := campaignRepo.GetByID(ctx, campaignID)
		if cancelledCamp.Status != domain.CampaignStatusCancelled {
			t.Errorf("expected DB status to be 'cancelled', got '%s'", cancelledCamp.Status)
		}

		// Test Delete
		reqDelete := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/admin/workspaces/%s/campaigns/%s", ws.ID, campaignID), nil)
		recDelete := httptest.NewRecorder()
		cDelete := e.NewContext(reqDelete, recDelete)
		cDelete.SetPath("/admin/workspaces/:workspace_id/campaigns/:id")
		cDelete.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
			{Name: "id", Value: campaignID.String()},
		})
		if err := h.Delete(cDelete); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
		if recDelete.Code != http.StatusOK {
			t.Errorf("Delete status expected 200, got %d", recDelete.Code)
		}

		deletedCamp, _ := campaignRepo.GetByID(ctx, campaignID)
		if deletedCamp != nil {
			t.Errorf("expected campaign to be deleted, but still exists")
		}
	})
}
