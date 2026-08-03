package admin

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

type CampaignHandler struct {
	CampaignRepo   *repository.CampaignRepository
	TemplateRepo   *repository.WABATemplateRepository
	ConnectionRepo *repository.ConnectionRepository
	TagRepo        *repository.TagRepository
	Publisher      *queue.JetStreamPublisher
}

func NewCampaignHandler(
	campaignRepo *repository.CampaignRepository,
	templateRepo *repository.WABATemplateRepository,
	connectionRepo *repository.ConnectionRepository,
	tagRepo *repository.TagRepository,
	publisher *queue.JetStreamPublisher,
) *CampaignHandler {
	return &CampaignHandler{
		CampaignRepo:   campaignRepo,
		TemplateRepo:   templateRepo,
		ConnectionRepo: connectionRepo,
		TagRepo:        tagRepo,
		Publisher:      publisher,
	}
}

func (h *CampaignHandler) List(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	campaigns, err := h.CampaignRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to list campaigns")
	}

	templates, err := h.TemplateRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		templates = []repository.WABATemplate{}
	}

	connections, err := h.ConnectionRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		connections = []*repository.Connection{}
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.CampaignsContent(workspaceID, campaigns, templates, connections))
	}
	return mw.Render(c, http.StatusOK, pages.CampaignsPage(workspaceID, campaigns, templates, connections))
}

func (h *CampaignHandler) NewForm(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	templates, err := h.TemplateRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		templates = []repository.WABATemplate{}
	}

	connections, err := h.ConnectionRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		connections = []*repository.Connection{}
	}

	return mw.Render(c, http.StatusOK, pages.CampaignCreateForm(workspaceID, templates, connections))
}

func (h *CampaignHandler) UploadCSV(c *echo.Context) error {
	fileHeader, err := c.FormFile("csv_file")
	if err != nil {
		return c.String(http.StatusBadRequest, "failed to read uploaded file")
	}

	src, err := fileHeader.Open()
	if err != nil {
		return c.String(http.StatusBadRequest, "failed to open uploaded file")
	}
	defer src.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, src); err != nil {
		return c.String(http.StatusBadRequest, "failed to read file content")
	}

	fileContent := buf.String()
	lines := strings.Split(fileContent, "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return c.String(http.StatusBadRequest, "uploaded file is empty")
	}

	delimiter := domain.SniffDelimiter(lines[0])

	r := csv.NewReader(strings.NewReader(fileContent))
	r.Comma = delimiter
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil {
		return c.String(http.StatusBadRequest, fmt.Sprintf("failed to parse CSV: %v", err))
	}

	if len(records) == 0 {
		return c.String(http.StatusBadRequest, "CSV contains no data")
	}

	// Read headers
	rawHeaders := records[0]
	headers := make([]string, len(rawHeaders))
	for i, h := range rawHeaders {
		headers[i] = strings.TrimSpace(strings.ToLower(h))
	}

	// We look for phone column
	phoneColIdx := -1
	phoneKeywords := []string{"phone", "telefone", "fone", "tel", "to", "number", "numero", "celular", "contato", "contact"}
	for i, h := range headers {
		for _, kw := range phoneKeywords {
			if strings.Contains(h, kw) {
				phoneColIdx = i
				break
			}
		}
		if phoneColIdx != -1 {
			break
		}
	}
	// Fallback to first column if no keyword matches
	if phoneColIdx == -1 {
		phoneColIdx = 0
	}

	// Track stats
	total := len(records) - 1 // minus header
	validCount := 0
	dupCount := 0
	invalidCount := 0

	seen := make(map[string]bool)
	var recipients []domain.CampaignRecipient
	var skipped []domain.SkippedRow

	var sampleRows [][]string
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) == 0 || (len(row) == 1 && row[0] == "") {
			total-- // skip empty lines
			continue
		}

		rawInput := strings.Join(row, string(delimiter))
		lineNumber := i + 1

		// Pad row if shorter than headers
		for len(row) < len(headers) {
			row = append(row, "")
		}

		phoneVal := row[phoneColIdx]
		cleanPhone, isValid := domain.SanitizePhone(phoneVal)

		if !isValid {
			invalidCount++
			skipped = append(skipped, domain.SkippedRow{
				LineNumber: lineNumber,
				RawInput:   rawInput,
				Reason:     fmt.Sprintf("numero de telefone invalido (tamanho %d)", len(cleanPhone)),
			})
			continue
		}

		if seen[cleanPhone] {
			dupCount++
			skipped = append(skipped, domain.SkippedRow{
				LineNumber: lineNumber,
				RawInput:   rawInput,
				Reason:     "numero de telefone duplicado",
			})
			continue
		}

		seen[cleanPhone] = true
		validCount++

		// Map variables
		variables := make(map[string]string)
		for colIdx, colVal := range row {
			if colIdx < len(headers) {
				variables[headers[colIdx]] = colVal
			}
			// Fallback to index-based keys
			variables[strconv.Itoa(colIdx)] = colVal
		}

		recipients = append(recipients, domain.CampaignRecipient{
			To:        cleanPhone,
			Variables: variables,
		})

		if len(sampleRows) < 5 {
			sampleRows = append(sampleRows, row)
		}
	}

	summary := map[string]int{
		"total":     total,
		"valid":     validCount,
		"duplicate": dupCount,
		"invalid":   invalidCount,
	}

	return mw.Render(c, http.StatusOK, pages.CSVPreviewSegment(summary, rawHeaders, sampleRows, recipients, skipped))
}

func (h *CampaignHandler) Create(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	name := c.FormValue("name")
	connectionIDStr := c.FormValue("channel")
	batchSizeStr := c.FormValue("batch_size")
	delayStr := c.FormValue("delay_seconds")

	var connectionID uuid.UUID
	var channel string
	var conn *repository.Connection

	if parsedID, err := uuid.Parse(connectionIDStr); err == nil {
		connectionID = parsedID
		conn, err = h.ConnectionRepo.GetByID(c.Request().Context(), connectionID)
		if err != nil {
			return c.String(http.StatusBadRequest, "connection not found")
		}
		channel = conn.Channel
	} else {
		// Fallback: treat connectionIDStr as channel name and get default connection
		conn, err = h.ConnectionRepo.GetDefaultChannelConnection(c.Request().Context(), workspaceID, connectionIDStr)
		if err != nil {
			return c.String(http.StatusBadRequest, fmt.Sprintf("no active connection found for channel %s: %v", connectionIDStr, err))
		}
		connectionID = conn.ID
		channel = conn.Channel
	}
	batchSize, _ := strconv.Atoi(batchSizeStr)
	if batchSize <= 0 {
		batchSize = 100
	}
	delaySeconds, _ := strconv.Atoi(delayStr)
	if delaySeconds < 0 {
		delaySeconds = 5
	}

	var templateName *string
	if channel == "whatsapp_cloud" {
		tName := c.FormValue("template_select")
		if tName != "" {
			templateName = &tName
		}
	} else {
		body := c.FormValue("body_template")
		if body != "" {
			templateName = &body
		}
	}

	recipientsRaw := c.FormValue("recipients_data")
	skippedRaw := c.FormValue("skipped_data")

	var recipients []domain.CampaignRecipient
	var skipped []domain.SkippedRow

	if recipientsRaw != "" {
		_ = json.Unmarshal([]byte(recipientsRaw), &recipients)
	}
	if skippedRaw != "" {
		_ = json.Unmarshal([]byte(skippedRaw), &skipped)
	}

	// Resolve template parameters from form if WABA template selected
	if channel == "whatsapp_cloud" && templateName != nil {
		// Iterate through FormParams to find waba_param_* mapping inputs
		// and map them into the variables map of each recipient
		for _, rec := range recipients {
			for i := 1; ; i++ {
				inputKey := fmt.Sprintf("waba_param_%d", i)
				mappedVal := c.FormValue(inputKey)
				if mappedVal == "" {
					break
				}
				// Resolve the input using the recipient's raw CSV columns
				resolvedVal := domain.ResolveVariables(mappedVal, rec.Variables)
				rec.Variables[strconv.Itoa(i)] = resolvedVal
			}
		}
	}

	camp := &domain.Campaign{
		WorkspaceID:  workspaceID,
		ConnectionID: &connectionID,
		Name:         name,
		Status:       domain.CampaignStatusDraft,
		BatchSize:    batchSize,
		DelaySeconds: delaySeconds,
		TemplateName: templateName,
		Channel:      &channel,
		Recipients:   recipients,
		SkippedRows:  skipped,
	}

	_, err = h.CampaignRepo.Create(c.Request().Context(), camp)
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to save campaign: %v", err))
	}

	// Redirect back to campaigns list page
	c.Response().Header().Set("HX-Redirect", fmt.Sprintf("/admin/workspaces/%s/campaigns", workspaceIDStr))
	return c.String(http.StatusOK, "")
}

func (h *CampaignHandler) DownloadSkipped(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}

	camp, err := h.CampaignRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusNotFound, "campaign not found")
	}

	c.Response().Header().Set("Content-Type", "text/csv")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=campanha_%s_rejeitados.csv", id.String()[:8]))
	c.Response().WriteHeader(http.StatusOK)

	writer := csv.NewWriter(c.Response())
	_ = writer.Write([]string{"Linha", "Registro Original", "Motivo da Rejeicao"})

	for _, row := range camp.SkippedRows {
		_ = writer.Write([]string{strconv.Itoa(row.LineNumber), row.RawInput, row.Reason})
	}
	writer.Flush()
	return nil
}

func (h *CampaignHandler) Start(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}

	ctx := c.Request().Context()
	camp, err := h.CampaignRepo.GetByID(ctx, id)
	if err != nil {
		return c.String(http.StatusNotFound, "campaign not found")
	}

	if camp.Status != domain.CampaignStatusDraft {
		return c.String(http.StatusBadRequest, "only campaigns in draft status can be started")
	}

	// Slice into batches
	recipients := camp.Recipients
	batchSize := camp.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	totalRecipients := len(recipients)
	var batches [][]domain.CampaignRecipient
	for i := 0; i < totalRecipients; i += batchSize {
		end := i + batchSize
		if end > totalRecipients {
			end = totalRecipients
		}
		batches = append(batches, recipients[i:end])
	}

	totalBatches := len(batches)
	if totalBatches == 0 {
		// Nothing to send, complete campaign immediately
		_ = h.CampaignRepo.UpdateStatus(ctx, id, domain.CampaignStatusCompleted)
		camp.Status = domain.CampaignStatusCompleted
		return mw.Render(c, http.StatusOK, pages.CampaignRow(camp.WorkspaceID, *camp))
	}

	// Update DB status to sending
	err = h.CampaignRepo.UpdateStatus(ctx, id, domain.CampaignStatusSending)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to update campaign status")
	}
	camp.Status = domain.CampaignStatusSending

	// Enqueue batches
	for idx, batch := range batches {
		task := queue.CampaignBatchTask{
			CampaignID:   camp.ID,
			WorkspaceID:  camp.WorkspaceID,
			BatchIndex:   idx + 1,
			TotalBatches: totalBatches,
			Recipients:   batch,
			DelaySeconds: camp.DelaySeconds,
		}
		payload, err := json.Marshal(task)
		if err != nil {
			return c.String(http.StatusInternalServerError, "failed to marshal batch task")
		}

		traceID := fmt.Sprintf("campaign_%s_batch_%d", camp.ID, idx+1)
		err = h.Publisher.Publish(ctx, "campaigns.batches", payload, traceID)
		if err != nil {
			return c.String(http.StatusInternalServerError, "failed to publish campaign batches")
		}
	}

	return mw.Render(c, http.StatusOK, pages.CampaignRow(camp.WorkspaceID, *camp))
}

func (h *CampaignHandler) Cancel(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}

	ctx := c.Request().Context()
	camp, err := h.CampaignRepo.GetByID(ctx, id)
	if err != nil {
		return c.String(http.StatusNotFound, "campaign not found")
	}

	if camp.Status != domain.CampaignStatusSending && camp.Status != domain.CampaignStatusScheduled {
		return c.String(http.StatusBadRequest, "only active or scheduled campaigns can be cancelled")
	}

	err = h.CampaignRepo.UpdateStatus(ctx, id, domain.CampaignStatusCancelled)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to cancel campaign")
	}
	camp.Status = domain.CampaignStatusCancelled

	return mw.Render(c, http.StatusOK, pages.CampaignRow(camp.WorkspaceID, *camp))
}

func (h *CampaignHandler) Delete(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}

	err = h.CampaignRepo.Delete(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete campaign")
	}

	return c.String(http.StatusOK, "")
}

// REST API Handlers

type CreateCampaignRequest struct {
	Name           string                     `json:"name"`
	ConnectionSlug string                     `json:"connection_slug"`
	TemplateName   *string                    `json:"template_name,omitempty"`
	MessageBody    *string                    `json:"message_body,omitempty"`
	TagID          *uuid.UUID                 `json:"tag_id,omitempty"`
	BatchSize      int                        `json:"batch_size,omitempty"`
	DelaySeconds   int                        `json:"delay_seconds,omitempty"`
	Recipients     []domain.CampaignRecipient `json:"recipients,omitempty"`
}

// APICreate handles campaign creation via JSON REST API with pre-flight validation.
func (h *CampaignHandler) APICreate(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	var req CreateCampaignRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
	}

	if req.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "campaign name is required"})
	}

	// 1. Pre-flight Connection Validation
	if req.ConnectionSlug == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "connection_slug is required"})
	}

	conn, err := h.ConnectionRepo.GetBySlug(c.Request().Context(), workspaceID, req.ConnectionSlug)
	if err != nil || conn == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("active connection with slug %q not found for workspace", req.ConnectionSlug)})
	}
	if conn.Status != "active" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("connection %q is currently %s", req.ConnectionSlug, conn.Status)})
	}

	// 2. Pre-flight Recipient Validation
	if len(req.Recipients) == 0 && req.TagID == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "campaign requires at least one recipient or a valid tag_id"})
	}

	var recipientRecords []domain.CampaignRecipientRecord
	var sampleRecipients []domain.CampaignRecipient

	// Resolve tag segment recipients if tag_id supplied
	if req.TagID != nil && h.TagRepo != nil {
		contacts, err := h.TagRepo.ListContactsByTag(c.Request().Context(), workspaceID, *req.TagID)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("failed to resolve contacts for tag: %v", err)})
		}
		for _, contact := range contacts {
			phone := ""
			for _, ident := range contact.Identities {
				if ident.SenderIdentity != "" {
					phone = ident.SenderIdentity
					break
				}
			}
			cleanPhone, valid := domain.SanitizePhone(phone)
			if !valid {
				continue
			}
			contactID := contact.ID
			recipientRecords = append(recipientRecords, domain.CampaignRecipientRecord{
				ContactID: &contactID,
				Phone:     cleanPhone,
				Status:    domain.RecipientStatusPending,
				Variables: map[string]string{"name": contact.Name},
			})
			sampleRecipients = append(sampleRecipients, domain.CampaignRecipient{
				To:        cleanPhone,
				Variables: map[string]string{"name": contact.Name},
			})
		}
	}

	// Resolve inline CSV/JSON recipients if provided
	for _, rec := range req.Recipients {
		cleanPhone, valid := domain.SanitizePhone(rec.To)
		if !valid {
			continue
		}
		recipientRecords = append(recipientRecords, domain.CampaignRecipientRecord{
			Phone:     cleanPhone,
			Status:    domain.RecipientStatusPending,
			Variables: rec.Variables,
		})
		sampleRecipients = append(sampleRecipients, domain.CampaignRecipient{
			To:        cleanPhone,
			Variables: rec.Variables,
		})
	}

	if len(recipientRecords) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no valid E.164 recipients resolved for campaign"})
	}

	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	delaySeconds := req.DelaySeconds
	if delaySeconds <= 0 {
		delaySeconds = 5
	}

	connID := conn.ID
	connSlug := conn.Slug
	channel := conn.Channel

	camp := &domain.Campaign{
		WorkspaceID:     workspaceID,
		ConnectionID:    &connID,
		ConnectionSlug:  &connSlug,
		Name:            req.Name,
		Status:          domain.CampaignStatusDraft,
		BatchSize:       batchSize,
		DelaySeconds:    delaySeconds,
		TemplateName:    req.TemplateName,
		MessageBody:     req.MessageBody,
		Channel:         &channel,
		TagID:           req.TagID,
		TotalRecipients: len(recipientRecords),
		Recipients:      sampleRecipients,
	}

	created, err := h.CampaignRepo.Create(c.Request().Context(), camp)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to save campaign: %v", err)})
	}

	// Save persisted recipient records in campaign_recipients
	if err := h.CampaignRepo.AddRecipients(c.Request().Context(), created.ID, recipientRecords); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to save campaign recipients: %v", err)})
	}

	return c.JSON(http.StatusCreated, created)
}

// APIList returns campaigns for a workspace as JSON.
func (h *CampaignHandler) APIList(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	campaigns, err := h.CampaignRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list campaigns"})
	}

	return c.JSON(http.StatusOK, campaigns)
}

// APIGet returns campaign detail and recipients as JSON.
func (h *CampaignHandler) APIGet(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid campaign ID"})
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid campaign ID"})
	}

	camp, err := h.CampaignRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "campaign not found"})
	}

	recipients, err := h.CampaignRepo.ListRecipients(c.Request().Context(), id, nil, 100)
	if err != nil {
		recipients = []domain.CampaignRecipientRecord{}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"campaign":   camp,
		"recipients": recipients,
	})
}

