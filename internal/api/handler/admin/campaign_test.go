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

	tagRepo := repository.NewTagRepository(pool)
	h := admin.NewCampaignHandler(campaignRepo, templateRepo, connectionRepo, tagRepo, pub)
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

	t.Run("Create Campaign Validation - No Recipients", func(t *testing.T) {
		form := url.Values{}
		form.Set("name", "Empty Recipients Campaign")
		form.Set("channel", "whatsapp")
		form.Set("batch_size", "50")
		form.Set("delay_seconds", "3")
		form.Set("body_template", "Test")

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
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected status 422, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "A campanha precisa de pelo menos um destinatário. Selecione uma tag ou envie um CSV.") {
			t.Errorf("expected validation error, got: %s", rec.Body.String())
		}
	})

	t.Run("Create Campaign Form - Multiple Tags tag_ids[]", func(t *testing.T) {
		tag1, err := tagRepo.CreateTag(ctx, ws.ID, "Tag Form 1", "#112233")
		if err != nil {
			t.Fatalf("failed to create tag1: %v", err)
		}
		tag2, err := tagRepo.CreateTag(ctx, ws.ID, "Tag Form 2", "#445566")
		if err != nil {
			t.Fatalf("failed to create tag2: %v", err)
		}

		form := url.Values{}
		form.Set("name", "Multi Tag Campaign Form")
		form.Set("channel", "whatsapp")
		form.Set("batch_size", "50")
		form.Set("delay_seconds", "3")
		form.Set("body_template", "Hello {{name}}")
		form.Add("tag_ids[]", tag1.ID.String())
		form.Add("tag_ids[]", tag2.ID.String())

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
			t.Fatalf("expected status 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		camps, err := campaignRepo.ListByWorkspace(ctx, ws.ID)
		if err != nil {
			t.Fatalf("ListByWorkspace failed: %v", err)
		}
		var found *domain.Campaign
		for i := range camps {
			if camps[i].Name == "Multi Tag Campaign Form" {
				found = &camps[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("Multi Tag Campaign Form not found in DB")
		}
		defer func() {
			_ = campaignRepo.Delete(ctx, found.ID)
		}()
		if len(found.TagIDs) != 2 {
			t.Fatalf("expected 2 tag_ids on campaign, got %d: %v", len(found.TagIDs), found.TagIDs)
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

	t.Run("REST API Campaign Endpoints", func(t *testing.T) {
		// 1. Create an active connection with slug
		connSlug := "waba-rest-test"
		err := connectionRepo.Create(ctx, &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WABA REST Test",
			Channel:        "whatsapp_cloud",
			Slug:           connSlug,
			SenderIdentity: "5511999990003",
			Status:         "active",
			IsDefault:      false,
		})
		if err != nil {
			t.Fatalf("failed to create connection with slug: %v", err)
		}

		// 2. Pre-flight Failure: Invalid connection slug
		invalidConnJSON := `{"name":"Fail Camp","connection_slug":"non-existent-slug","recipients":[{"to":"5511999998888"}]}`
		reqFail1 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/campaigns", ws.ID), strings.NewReader(invalidConnJSON))
		reqFail1.Header.Set("Content-Type", "application/json")
		recFail1 := httptest.NewRecorder()
		cFail1 := e.NewContext(reqFail1, recFail1)
		cFail1.SetPath("/api/v1/workspaces/:workspace_id/campaigns")
		cFail1.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		if err := h.APICreate(cFail1); err != nil {
			t.Fatalf("APICreate failed: %v", err)
		}
		if recFail1.Code != http.StatusBadRequest {
			t.Errorf("expected status 400 for invalid connection, got %d", recFail1.Code)
		}

		// 3. Pre-flight Failure: No recipients
		noRecipientsJSON := fmt.Sprintf(`{"name":"No Recip Camp","connection_slug":"%s"}`, connSlug)
		reqFail2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/campaigns", ws.ID), strings.NewReader(noRecipientsJSON))
		reqFail2.Header.Set("Content-Type", "application/json")
		recFail2 := httptest.NewRecorder()
		cFail2 := e.NewContext(reqFail2, recFail2)
		cFail2.SetPath("/api/v1/workspaces/:workspace_id/campaigns")
		cFail2.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		if err := h.APICreate(cFail2); err != nil {
			t.Fatalf("APICreate failed: %v", err)
		}
		if recFail2.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected status 422 for missing recipients, got %d", recFail2.Code)
		}

		// 4. Valid Creation
		validJSON := fmt.Sprintf(`{
			"name": "Black Friday Promo",
			"connection_slug": "%s",
			"message_body": "Ola {{name}}! Promocao exclusiva.",
			"batch_size": 100,
			"delay_seconds": 2,
			"recipients": [
				{"to": "5511999998888", "variables": {"name": "Carlos"}},
				{"to": "5511977776666", "variables": {"name": "Ana"}}
			]
		}`, connSlug)

		reqValid := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/campaigns", ws.ID), strings.NewReader(validJSON))
		reqValid.Header.Set("Content-Type", "application/json")
		recValid := httptest.NewRecorder()
		cValid := e.NewContext(reqValid, recValid)
		cValid.SetPath("/api/v1/workspaces/:workspace_id/campaigns")
		cValid.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		if err := h.APICreate(cValid); err != nil {
			t.Fatalf("APICreate valid failed: %v", err)
		}
		if recValid.Code != http.StatusCreated {
			t.Fatalf("expected status 201 Created, got %d (body: %s)", recValid.Code, recValid.Body.String())
		}

		var createdCamp domain.Campaign
		if err := json.Unmarshal(recValid.Body.Bytes(), &createdCamp); err != nil {
			t.Fatalf("failed to unmarshal created campaign: %v", err)
		}

		if createdCamp.Name != "Black Friday Promo" {
			t.Errorf("expected campaign name 'Black Friday Promo', got %s", createdCamp.Name)
		}

		// 5. APIList
		reqList := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/campaigns", ws.ID), nil)
		recList := httptest.NewRecorder()
		cList := e.NewContext(reqList, recList)
		cList.SetPath("/api/v1/workspaces/:workspace_id/campaigns")
		cList.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		if err := h.APIList(cList); err != nil {
			t.Fatalf("APIList failed: %v", err)
		}
		if recList.Code != http.StatusOK {
			t.Errorf("expected status 200 for APIList, got %d", recList.Code)
		}

		// 6. APIGet
		reqGet := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/campaigns/%s", ws.ID, createdCamp.ID), nil)
		recGet := httptest.NewRecorder()
		cGet := e.NewContext(reqGet, recGet)
		cGet.SetPath("/api/v1/workspaces/:workspace_id/campaigns/:id")
		cGet.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
			{Name: "id", Value: createdCamp.ID.String()},
		})

		if err := h.APIGet(cGet); err != nil {
			t.Fatalf("APIGet failed: %v", err)
		}
		if recGet.Code != http.StatusOK {
			t.Errorf("expected status 200 for APIGet, got %d", recGet.Code)
		}
	})

	t.Run("Tag Filtering and Selector", func(t *testing.T) {
		contactRepo := repository.NewContactRepository(pool)

		_ = connectionRepo.Create(ctx, &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WABA REST Test",
			Channel:        "whatsapp_cloud",
			Slug:           "waba-rest-test",
			SenderIdentity: "5511999990003",
			Status:         "active",
			IsDefault:      false,
		})

		// 1. Create a tag and a tagged contact
		tag, err := tagRepo.CreateTag(ctx, ws.ID, "VIP Customers", "#FF0000")
		if err != nil {
			t.Fatalf("failed to create tag: %v", err)
		}

		contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "5511988887777", "VIP John", "", "")
		if err != nil {
			t.Fatalf("failed to create contact: %v", err)
		}

		if err := tagRepo.AddTagToContact(ctx, ws.ID, contact.ID, tag.ID); err != nil {
			t.Fatalf("failed to add tag to contact: %v", err)
		}

		// 2. Test NewForm includes tag in dropdown
		reqForm := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/campaigns/new", ws.ID), nil)
		recForm := httptest.NewRecorder()
		cForm := e.NewContext(reqForm, recForm)
		cForm.SetPath("/admin/workspaces/:workspace_id/campaigns/new")
		cForm.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		if err := h.NewForm(cForm); err != nil {
			t.Fatalf("NewForm failed: %v", err)
		}
		if !strings.Contains(recForm.Body.String(), "VIP Customers") {
			t.Errorf("expected form HTML to contain tag name 'VIP Customers', got: %s", recForm.Body.String())
		}
		if !strings.Contains(recForm.Body.String(), "tag_id") {
			t.Errorf("expected form HTML to contain tag_id select dropdown")
		}

		// 3. Test Form Create with tag_id only (no CSV) — deferred tag resolution
		form := url.Values{}
		form.Set("name", "VIP Tag Campaign")
		form.Set("channel", "whatsapp")
		form.Set("tag_id", tag.ID.String())
		form.Set("batch_size", "100")
		form.Set("delay_seconds", "1")

		reqCreate := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/campaigns", ws.ID), strings.NewReader(form.Encode()))
		reqCreate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recCreate := httptest.NewRecorder()
		cCreate := e.NewContext(reqCreate, recCreate)
		cCreate.SetPath("/admin/workspaces/:workspace_id/campaigns")
		cCreate.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		if err := h.Create(cCreate); err != nil {
			t.Fatalf("Create with tag_id failed: %v", err)
		}
		if recCreate.Code != http.StatusOK {
			t.Errorf("expected 200 OK for Create with tag_id, got %d", recCreate.Code)
		}

		// Verify campaign row has tag_ids stored, but NO recipients resolved at creation time
		camps, _ := campaignRepo.ListByWorkspace(ctx, ws.ID)
		var vipCamp *domain.Campaign
		for i := range camps {
			if camps[i].Name == "VIP Tag Campaign" {
				vipCamp = &camps[i]
				break
			}
		}
		if vipCamp == nil {
			t.Fatalf("VIP Tag Campaign not found in DB")
		}
		// Tag IDs must be stored on the campaign row
		if len(vipCamp.TagIDs) != 1 || vipCamp.TagIDs[0] != tag.ID {
			t.Errorf("expected tag_ids [%s] on campaign, got: %v", tag.ID, vipCamp.TagIDs)
		}
		// Recipients JSONB should be empty (no CSV was provided)
		if len(vipCamp.Recipients) != 0 {
			t.Errorf("expected 0 recipients at creation time (deferred), got: %d", len(vipCamp.Recipients))
		}
		// campaign_recipients table should be empty
		recipientRecords, _ := campaignRepo.ListRecipients(ctx, vipCamp.ID, nil, 100)
		if len(recipientRecords) != 0 {
			t.Errorf("expected 0 campaign_recipients at creation time (deferred), got: %d", len(recipientRecords))
		}

		// 4. Test REST APICreate with tag_ids + inline recipients (deferred tag resolution)
		apiTag, err := tagRepo.CreateTag(ctx, ws.ID, "API Segment", "#00FF00")
		if err != nil {
			t.Fatalf("failed to create apiTag: %v", err)
		}
		contact2, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "5511977776666", "Segment Alice", "", "")
		if err != nil {
			t.Fatalf("failed to create contact2: %v", err)
		}
		if err := tagRepo.AddTagToContact(ctx, ws.ID, contact2.ID, apiTag.ID); err != nil {
			t.Fatalf("failed to tag contact2: %v", err)
		}

		apiPayload := fmt.Sprintf(`{
			"name": "API Tagged Campaign",
			"connection_slug": "waba-rest-test",
			"tag_ids": ["%s"],
			"recipients": [
				{"to": "5511977776666", "variables": {"name": "Duplicate Alice"}},
				{"to": "5511966665555", "variables": {"name": "New Bob"}}
			]
		}`, apiTag.ID)

		reqAPI := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/campaigns", ws.ID), strings.NewReader(apiPayload))
		reqAPI.Header.Set("Content-Type", "application/json")
		recAPI := httptest.NewRecorder()
		cAPI := e.NewContext(reqAPI, recAPI)
		cAPI.SetPath("/api/v1/workspaces/:workspace_id/campaigns")
		cAPI.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		if err := h.APICreate(cAPI); err != nil {
			t.Fatalf("APICreate with tag_ids failed: %v", err)
		}
		if recAPI.Code != http.StatusCreated {
			t.Fatalf("expected status 201 for APICreate with tag_ids, got %d: %s", recAPI.Code, recAPI.Body.String())
		}

		var createdAPICamp domain.Campaign
		if err := json.Unmarshal(recAPI.Body.Bytes(), &createdAPICamp); err != nil {
			t.Fatalf("failed to unmarshal campaign: %v", err)
		}

		// At creation time, only inline recipients are stored (tags are deferred).
		// 2 inline recipients — deduplication happens at execution time, not creation.
		if createdAPICamp.TotalRecipients != 2 {
			t.Errorf("expected 2 total recipients (inline only, tags deferred), got %d", createdAPICamp.TotalRecipients)
		}
		// Tag IDs must be stored on the campaign row
		if len(createdAPICamp.TagIDs) != 1 || createdAPICamp.TagIDs[0] != apiTag.ID {
			t.Errorf("expected tag_ids [%s] on campaign, got: %v", apiTag.ID, createdAPICamp.TagIDs)
		}
		// 5. Test REST APICreate with tags only (no recipients) — deferred tag resolution
		tagsOnlyPayload := fmt.Sprintf(`{
			"name": "Tags Only Campaign",
			"connection_slug": "waba-rest-test",
			"tag_ids": ["%s"]
		}`, apiTag.ID)

		reqTagsOnly := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/campaigns", ws.ID), strings.NewReader(tagsOnlyPayload))
		reqTagsOnly.Header.Set("Content-Type", "application/json")
		recTagsOnly := httptest.NewRecorder()
		cTagsOnly := e.NewContext(reqTagsOnly, recTagsOnly)
		cTagsOnly.SetPath("/api/v1/workspaces/:workspace_id/campaigns")
		cTagsOnly.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		if err := h.APICreate(cTagsOnly); err != nil {
			t.Fatalf("APICreate with tags only failed: %v", err)
		}
		if recTagsOnly.Code != http.StatusCreated {
			t.Fatalf("expected status 201 for APICreate with tags only, got %d: %s", recTagsOnly.Code, recTagsOnly.Body.String())
		}

		var tagsOnlyCamp domain.Campaign
		if err := json.Unmarshal(recTagsOnly.Body.Bytes(), &tagsOnlyCamp); err != nil {
			t.Fatalf("failed to unmarshal tags-only campaign: %v", err)
		}
		// Tag IDs stored
		if len(tagsOnlyCamp.TagIDs) != 1 || tagsOnlyCamp.TagIDs[0] != apiTag.ID {
			t.Errorf("expected tag_ids [%s] on tags-only campaign, got: %v", apiTag.ID, tagsOnlyCamp.TagIDs)
		}
		// No inline recipients
		if tagsOnlyCamp.TotalRecipients != 0 {
			t.Errorf("expected 0 total recipients for tags-only campaign, got %d", tagsOnlyCamp.TotalRecipients)
		}
		// campaign_recipients table empty
		tagsOnlyRecords, _ := campaignRepo.ListRecipients(ctx, tagsOnlyCamp.ID, nil, 100)
		if len(tagsOnlyRecords) != 0 {
			t.Errorf("expected 0 campaign_recipients for tags-only campaign, got: %d", len(tagsOnlyRecords))
		}

		// 6. Test: create campaign with neither tags nor CSV → HTTP 422
		noSourceJSON := `{"name":"Empty Sources","connection_slug":"waba-rest-test"}`
		reqNoSource := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/campaigns", ws.ID), strings.NewReader(noSourceJSON))
		reqNoSource.Header.Set("Content-Type", "application/json")
		recNoSource := httptest.NewRecorder()
		cNoSource := e.NewContext(reqNoSource, recNoSource)
		cNoSource.SetPath("/api/v1/workspaces/:workspace_id/campaigns")
		cNoSource.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		if err := h.APICreate(cNoSource); err != nil {
			t.Fatalf("APICreate no-source failed: %v", err)
		}
		if recNoSource.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected status 422 for no recipients and no tags, got %d", recNoSource.Code)
		}
	})
}

