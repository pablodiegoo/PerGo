package admin

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/campaign"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
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
	Engine         campaign.BroadcasterEngine
}

func NewCampaignHandler(
	campaignRepo *repository.CampaignRepository,
	templateRepo *repository.WABATemplateRepository,
	connectionRepo *repository.ConnectionRepository,
	tagRepo *repository.TagRepository,
	publisher *queue.JetStreamPublisher,
	engine ...campaign.BroadcasterEngine,
) *CampaignHandler {
	h := &CampaignHandler{
		CampaignRepo:   campaignRepo,
		TemplateRepo:   templateRepo,
		ConnectionRepo: connectionRepo,
		TagRepo:        tagRepo,
		Publisher:      publisher,
	}
	if len(engine) > 0 && engine[0] != nil {
		h.Engine = engine[0]
	} else {
		_ = h.ensureEngine()
	}
	return h
}

func (h *CampaignHandler) WithEngine(engine campaign.BroadcasterEngine) *CampaignHandler {
	h.Engine = engine
	return h
}

func (h *CampaignHandler) ensureEngine() campaign.BroadcasterEngine {
	if h.Engine != nil {
		return h.Engine
	}
	if h.CampaignRepo != nil {
		var pub campaign.Publisher
		if h.Publisher != nil {
			pub = h.Publisher
		} else {
			pub = noopPublisher{}
		}
		var tagLister domain.TagContactLister
		if h.TagRepo != nil {
			tagLister = h.TagRepo
		}
		h.Engine = campaign.NewBroadcasterEngine(h.CampaignRepo, h.ConnectionRepo, nil, pub, nil, tagLister)
	}
	return h.Engine
}

func handleHTMXEngineError(c *echo.Context, err error, defaultMsg string) error {
	var transErr domain.ErrInvalidCampaignTransition
	if errors.As(err, &transErr) {
		trigger := fmt.Sprintf(`{"showToast":{"level":"error","message":%q}}`, transErr.Error())
		c.Response().Header().Set("HX-Trigger", trigger)
		return c.String(http.StatusConflict, transErr.Error())
	}
	if errors.Is(err, repository.ErrCampaignNotFound) {
		return c.String(http.StatusNotFound, "campaign not found")
	}
	if errors.Is(err, repository.ErrConnectionNotFound) || strings.Contains(err.Error(), "connection") {
		return c.String(http.StatusBadRequest, err.Error())
	}
	if strings.Contains(err.Error(), "requires at least one recipient") {
		return c.String(http.StatusUnprocessableEntity, "A campanha precisa de pelo menos um destinatário. Selecione uma tag ou envie um CSV.")
	}
	if strings.Contains(err.Error(), "cannot delete campaign in") {
		return c.String(http.StatusBadRequest, err.Error())
	}
	if defaultMsg != "" {
		return c.String(http.StatusInternalServerError, defaultMsg)
	}
	return c.String(http.StatusInternalServerError, err.Error())
}

func handleRESTEngineError(c *echo.Context, err error, defaultMsg string) error {
	var transErr domain.ErrInvalidCampaignTransition
	if errors.As(err, &transErr) {
		return c.JSON(http.StatusConflict, map[string]string{
			"code":    transErr.Code(),
			"message": transErr.Error(),
		})
	}
	if errors.Is(err, repository.ErrCampaignNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "campaign not found"})
	}
	if errors.Is(err, repository.ErrConnectionNotFound) || strings.Contains(err.Error(), "connection") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if strings.Contains(err.Error(), "requires at least one recipient") {
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	}
	if strings.Contains(err.Error(), "cannot delete campaign in") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if defaultMsg != "" {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": defaultMsg})
	}
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
}

type noopPublisher struct{}

func (noopPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	return nil
}

func resolveScope(c *echo.Context) (domain.WorkspaceScope, error) {
	var scope domain.WorkspaceScope
	var ok bool

	if s, err := domain.Require(c.Request().Context()); err == nil && (s.WorkspaceID() != uuid.Nil || s.IsOperator()) {
		scope = s
		ok = true
	} else if id, hasTenant := tenant.WorkspaceIDFrom(c.Request().Context()); hasTenant && id != uuid.Nil {
		scope = domain.NewWorkspaceScope(id, domain.CapabilityWorkspaceScoped)
		ok = true
	}

	if !ok {
		return domain.WorkspaceScope{}, domain.ErrMissingScope
	}

	if paramWS, pErr := echo.PathParam[string](c, "workspace_id"); pErr == nil && paramWS != "" {
		if parsedWS, err := uuid.Parse(paramWS); err == nil && parsedWS != uuid.Nil {
			if !scope.Matches(parsedWS) {
				return domain.WorkspaceScope{}, repository.ErrCampaignNotFound
			}
			if scope.IsOperator() {
				scope = scope.WithTarget(parsedWS)
			}
		} else if err != nil {
			return domain.WorkspaceScope{}, fmt.Errorf("invalid workspace ID")
		}
	}

	return scope, nil
}

func (h *CampaignHandler) List(c *echo.Context) error {
	scope, err := resolveScope(c)
	if err != nil || scope.WorkspaceID() == uuid.Nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID := scope.WorkspaceID()

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
	scope, err := resolveScope(c)
	if err != nil || scope.WorkspaceID() == uuid.Nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID := scope.WorkspaceID()

	templates, err := h.TemplateRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		templates = []repository.WABATemplate{}
	}

	connections, err := h.ConnectionRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		connections = []*repository.Connection{}
	}

	var tags []domain.Tag
	if h.TagRepo != nil {
		var tagErr error
		tags, tagErr = h.TagRepo.ListTags(c.Request().Context(), workspaceID)
		if tagErr != nil {
			tags = []domain.Tag{}
		}
	} else {
		tags = []domain.Tag{}
	}

	return mw.Render(c, http.StatusOK, pages.CampaignCreateForm(workspaceID, templates, connections, tags))
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

func parseScheduledAt(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	formats := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	}
	for _, layout := range formats {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			utc := t.UTC()
			return &utc, nil
		}
		if t, err := time.Parse(layout, s); err == nil {
			utc := t.UTC()
			return &utc, nil
		}
	}
	return nil, fmt.Errorf("invalid scheduled_at format")
}

func (h *CampaignHandler) Create(c *echo.Context) error {
	rateLimitStr := strings.TrimSpace(c.FormValue("rate_limit_per_min"))
	var rateLimitPerMin *int
	if rateLimitStr != "" {
		rl, err := strconv.Atoi(rateLimitStr)
		if err != nil || rl <= 0 {
			return c.String(http.StatusBadRequest, "rate_limit_per_min must be greater than 0")
		}
		rateLimitPerMin = &rl
	}

	scheduledAtStr := strings.TrimSpace(c.FormValue("scheduled_at"))
	var scheduledAt *time.Time
	if scheduledAtStr != "" {
		parsed, err := parseScheduledAt(scheduledAtStr)
		if err != nil {
			return c.String(http.StatusBadRequest, "formato invalido para agendamento")
		}
		scheduledAt = parsed
	}

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.String(http.StatusNotFound, "campaign not found")
		}
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	name := c.FormValue("name")
	connectionIDStr := c.FormValue("channel")
	batchSizeStr := c.FormValue("batch_size")
	delayStr := c.FormValue("delay_seconds")

	var connectionID *uuid.UUID
	var channel *string
	var connectionSlug string

	if parsedID, err := uuid.Parse(connectionIDStr); err == nil {
		connectionID = &parsedID
	} else if connectionIDStr == "whatsapp" || connectionIDStr == "whatsapp_cloud" || connectionIDStr == "telegram" {
		channel = &connectionIDStr
	} else if connectionIDStr != "" {
		connectionSlug = connectionIDStr
		channel = &connectionIDStr
	}

	batchSize, _ := strconv.Atoi(batchSizeStr)
	delaySeconds, _ := strconv.Atoi(delayStr)

	var formTagIDs []uuid.UUID
	if tagIDStr := c.FormValue("tag_id"); tagIDStr != "" {
		if parsedID, err := uuid.Parse(tagIDStr); err == nil {
			formTagIDs = append(formTagIDs, parsedID)
		}
	}
	_ = c.Request().ParseForm()
	for _, key := range []string{"tag_ids", "tag_ids[]"} {
		if tagIDStrings, ok := c.Request().Form[key]; ok {
			for _, tagIDStr := range tagIDStrings {
				if parsedID, err := uuid.Parse(tagIDStr); err == nil {
					formTagIDs = append(formTagIDs, parsedID)
				}
			}
		}
	}
	formTagIDs = domain.DeduplicateUUIDs(formTagIDs)

	recipientsRaw := c.FormValue("recipients_data")
	skippedRaw := c.FormValue("skipped_data")

	var recipients []domain.CampaignRecipient
	var skipped []domain.SkippedRow

	if recipientsRaw != "" {
		if err := json.Unmarshal([]byte(recipientsRaw), &recipients); err != nil {
			slog.Warn("failed to parse recipients_data form value", "error", err)
			return c.String(http.StatusBadRequest, "invalid recipients data format")
		}
	}
	if skippedRaw != "" {
		if err := json.Unmarshal([]byte(skippedRaw), &skipped); err != nil {
			slog.Warn("failed to parse skipped_data form value", "error", err)
			return c.String(http.StatusBadRequest, "invalid skipped data format")
		}
	}

	if len(formTagIDs) == 0 && len(recipients) == 0 {
		return c.String(http.StatusUnprocessableEntity, "A campanha precisa de pelo menos um destinatário. Selecione uma tag ou envie um CSV.")
	}

	var templateName *string
	var messageBody *string
	tName := c.FormValue("template_select")
	bTemplate := c.FormValue("body_template")
	if tName != "" {
		templateName = &tName
	}
	if bTemplate != "" {
		messageBody = &bTemplate
		if templateName == nil {
			templateName = &bTemplate
		}
	}

	if tName != "" {
		for _, rec := range recipients {
			for i := 1; ; i++ {
				inputKey := fmt.Sprintf("waba_param_%d", i)
				mappedVal := c.FormValue(inputKey)
				if mappedVal == "" {
					break
				}
				resolvedVal := domain.ResolveVariables(mappedVal, rec.Variables)
				rec.Variables[strconv.Itoa(i)] = resolvedVal
			}
		}
	}

	var formFallbackChannels []string
	if fcStr := c.FormValue("fallback_channels"); fcStr != "" {
		for _, part := range strings.Split(fcStr, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				formFallbackChannels = append(formFallbackChannels, part)
			}
		}
	}
	for _, key := range []string{"fallback_channels", "fallback_channels[]"} {
		if fcSlice, ok := c.Request().Form[key]; ok {
			for _, ch := range fcSlice {
				ch = strings.TrimSpace(ch)
				if ch != "" {
					formFallbackChannels = append(formFallbackChannels, ch)
				}
			}
		}
	}
	formFallbackChannels = domain.DeduplicateStrings(formFallbackChannels)

	var formFallbackBehavior *string
	if fb := strings.TrimSpace(c.FormValue("fallback_behavior")); fb != "" {
		formFallbackBehavior = &fb
	}

	var formInteractive *domain.Interactive
	if interRaw := strings.TrimSpace(c.FormValue("interactive_data")); interRaw != "" && interRaw != "{}" && interRaw != "null" {
		var inter domain.Interactive
		if err := json.Unmarshal([]byte(interRaw), &inter); err == nil && inter.Type != "" {
			formInteractive = &inter
		}
	}

	eng := h.ensureEngine()
	if eng == nil {
		return c.String(http.StatusInternalServerError, "campaign engine unavailable")
	}

	params := campaign.CreateCampaignParams{
		Name:             name,
		ConnectionSlug:   connectionSlug,
		ConnectionID:     connectionID,
		Channel:          channel,
		BatchSize:        batchSize,
		DelaySeconds:     delaySeconds,
		RateLimitPerMin:  rateLimitPerMin,
		ScheduledAt:      scheduledAt,
		TemplateName:     templateName,
		MessageBody:      messageBody,
		FallbackChannels: formFallbackChannels,
		Interactive:      formInteractive,
		FallbackBehavior: formFallbackBehavior,
		TagIDs:           formTagIDs,
		Recipients:       recipients,
		SkippedRows:      skipped,
	}

	_, err = eng.Create(c.Request().Context(), scope, params)
	if err != nil {
		return handleHTMXEngineError(c, err, fmt.Sprintf("failed to save campaign: %v", err))
	}

	c.Response().Header().Set("HX-Redirect", fmt.Sprintf("/admin/workspaces/%s/campaigns", scope.WorkspaceID().String()))
	return c.String(http.StatusOK, "")
}

func writeSkippedRowsCSV(w io.Writer, skippedRows []domain.SkippedRow) error {
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"Linha", "Registro Original", "Motivo da Rejeicao"}); err != nil {
		return err
	}

	for _, row := range skippedRows {
		if err := writer.Write([]string{strconv.Itoa(row.LineNumber), row.RawInput, row.Reason}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
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

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.String(http.StatusNotFound, "campaign not found")
		}
		return c.String(http.StatusUnauthorized, "unauthorized")
	}

	camp, err := h.CampaignRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusNotFound, "campaign not found")
	}
	if !scope.Matches(camp.WorkspaceID) {
		return c.String(http.StatusNotFound, "campaign not found")
	}

	c.Response().Header().Set("Content-Type", "text/csv")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=campanha_%s_rejeitados.csv", id.String()[:8]))
	c.Response().WriteHeader(http.StatusOK)

	return writeSkippedRowsCSV(c.Response(), camp.SkippedRows)
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

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.String(http.StatusNotFound, "campaign not found")
		}
		return c.String(http.StatusUnauthorized, "unauthorized")
	}

	eng := h.ensureEngine()
	if eng == nil {
		return c.String(http.StatusInternalServerError, "campaign engine unavailable")
	}

	camp, err := eng.Start(c.Request().Context(), scope, id)
	if err != nil {
		return handleHTMXEngineError(c, err, "")
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

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.String(http.StatusNotFound, "campaign not found")
		}
		return c.String(http.StatusUnauthorized, "unauthorized")
	}

	eng := h.ensureEngine()
	if eng == nil {
		return c.String(http.StatusInternalServerError, "campaign engine unavailable")
	}

	camp, err := eng.Cancel(c.Request().Context(), scope, id)
	if err != nil {
		return handleHTMXEngineError(c, err, "failed to cancel campaign")
	}

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

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.String(http.StatusNotFound, "campaign not found")
		}
		return c.String(http.StatusUnauthorized, "unauthorized")
	}

	eng := h.ensureEngine()
	if eng == nil {
		return c.String(http.StatusInternalServerError, "campaign engine unavailable")
	}

	err = eng.Delete(c.Request().Context(), scope, id)
	if err != nil {
		return handleHTMXEngineError(c, err, "failed to delete campaign")
	}

	return c.String(http.StatusOK, "")
}

func (h *CampaignHandler) Pause(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.String(http.StatusNotFound, "campaign not found")
		}
		return c.String(http.StatusUnauthorized, "unauthorized")
	}

	eng := h.ensureEngine()
	if eng == nil {
		return c.String(http.StatusInternalServerError, "campaign engine unavailable")
	}

	camp, err := eng.Pause(c.Request().Context(), scope, id)
	if err != nil {
		return handleHTMXEngineError(c, err, "failed to pause campaign")
	}

	return mw.Render(c, http.StatusOK, pages.CampaignRow(camp.WorkspaceID, *camp))
}

func (h *CampaignHandler) Resume(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.String(http.StatusNotFound, "campaign not found")
		}
		return c.String(http.StatusUnauthorized, "unauthorized")
	}

	eng := h.ensureEngine()
	if eng == nil {
		return c.String(http.StatusInternalServerError, "campaign engine unavailable")
	}

	camp, err := eng.Resume(c.Request().Context(), scope, id)
	if err != nil {
		return handleHTMXEngineError(c, err, "failed to resume campaign")
	}

	return mw.Render(c, http.StatusOK, pages.CampaignRow(camp.WorkspaceID, *camp))
}

func (h *CampaignHandler) GetRow(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid campaign ID")
	}

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.String(http.StatusNotFound, "campaign not found")
		}
		return c.String(http.StatusUnauthorized, "unauthorized")
	}

	camp, err := h.CampaignRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusNotFound, "campaign not found")
	}
	if !scope.Matches(camp.WorkspaceID) {
		return c.String(http.StatusNotFound, "campaign not found")
	}

	return mw.Render(c, http.StatusOK, pages.CampaignRow(camp.WorkspaceID, *camp))
}

// REST API Handlers

type CreateCampaignRequest struct {
	Name             string                     `json:"name"`
	ConnectionSlug   string                     `json:"connection_slug"`
	ConnectionID     *uuid.UUID                 `json:"connection_id,omitempty"`
	TemplateName     *string                    `json:"template_name,omitempty"`
	MessageBody      *string                    `json:"message_body,omitempty"`
	TagID            *uuid.UUID                 `json:"tag_id,omitempty"`
	TagIDs           []uuid.UUID                `json:"tag_ids,omitempty"`
	BatchSize        int                        `json:"batch_size,omitempty"`
	DelaySeconds     int                        `json:"delay_seconds,omitempty"`
	RateLimitPerMin  *int                       `json:"rate_limit_per_min,omitempty"`
	ScheduledAt      *time.Time                 `json:"scheduled_at,omitempty"`
	Status           *domain.CampaignStatus     `json:"status,omitempty"`
	Recipients       []domain.CampaignRecipient `json:"recipients,omitempty"`
	FallbackChannels []string                   `json:"fallback_channels,omitempty"`
	Interactive      *domain.Interactive        `json:"interactive,omitempty"`
	FallbackBehavior *string                    `json:"fallback_behavior,omitempty"`
	Channel          *string                    `json:"channel,omitempty"`
}

// APICreate handles campaign creation via JSON REST API with pre-flight validation.
func (h *CampaignHandler) APICreate(c *echo.Context) error {
	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "campaign not found"})
		}
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "INVALID_WORKSPACE_ID",
			"message": "invalid workspace ID",
			"error":   "invalid workspace ID",
		})
	}

	var req CreateCampaignRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
	}

	if req.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "campaign name is required"})
	}
	if req.RateLimitPerMin != nil && *req.RateLimitPerMin <= 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "rate_limit_per_min must be greater than 0"})
	}
	if req.FallbackBehavior != nil && *req.FallbackBehavior != "" {
		if *req.FallbackBehavior != "degrade" && *req.FallbackBehavior != "fail" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": `fallback_behavior must be either "degrade" or "fail"`})
		}
	}
	if req.Interactive != nil {
		if req.Interactive.Type == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "interactive.type is required"})
		}
		if req.Interactive.Body.Text == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "interactive.body.text is required"})
		}
		if req.Interactive.Type == "button" && len(req.Interactive.Action.Buttons) == 0 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "interactive.action.buttons is required when type is button"})
		}
		if req.Interactive.Type == "list" && len(req.Interactive.Action.Sections) == 0 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "interactive.action.sections is required when type is list"})
		}
	}
	if req.ConnectionSlug == "" && req.ConnectionID == nil && req.Channel == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "connection_slug is required"})
	}

	targetTagIDs := append([]uuid.UUID{}, req.TagIDs...)
	if req.TagID != nil && *req.TagID != uuid.Nil {
		targetTagIDs = append(targetTagIDs, *req.TagID)
	}
	targetTagIDs = domain.DeduplicateUUIDs(targetTagIDs)

	if len(req.Recipients) == 0 && len(targetTagIDs) == 0 {
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "campaign requires at least one recipient or a valid tag_id/tag_ids"})
	}

	eng := h.ensureEngine()
	if eng == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "campaign engine unavailable"})
	}

	params := campaign.CreateCampaignParams{
		Name:             req.Name,
		ConnectionSlug:   req.ConnectionSlug,
		ConnectionID:     req.ConnectionID,
		Channel:          req.Channel,
		BatchSize:        req.BatchSize,
		DelaySeconds:     req.DelaySeconds,
		RateLimitPerMin:  req.RateLimitPerMin,
		ScheduledAt:      req.ScheduledAt,
		TemplateName:     req.TemplateName,
		MessageBody:      req.MessageBody,
		FallbackChannels: req.FallbackChannels,
		Interactive:      req.Interactive,
		FallbackBehavior: req.FallbackBehavior,
		TagID:            req.TagID,
		TagIDs:           req.TagIDs,
		Recipients:       req.Recipients,
	}

	created, err := eng.Create(c.Request().Context(), scope, params)
	if err != nil {
		if strings.Contains(err.Error(), "required") ||
			strings.Contains(err.Error(), "rate_limit_per_min") ||
			strings.Contains(err.Error(), "fallback_behavior") ||
			strings.Contains(err.Error(), "interactive") {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return handleRESTEngineError(c, err, "")
	}

	return c.JSON(http.StatusCreated, created)
}

// APIList returns campaigns for a workspace as JSON.
func (h *CampaignHandler) APIList(c *echo.Context) error {
	scope, err := resolveScope(c)
	if err != nil || scope.WorkspaceID() == uuid.Nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	campaigns, err := h.CampaignRepo.ListByWorkspace(c.Request().Context(), scope.WorkspaceID())
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

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "campaign not found"})
		}
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	camp, err := h.CampaignRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "campaign not found"})
	}
	if !scope.Matches(camp.WorkspaceID) {
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

// APIStart starts a campaign via REST API.
func (h *CampaignHandler) APIStart(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid campaign ID"})
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid campaign ID"})
	}

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "campaign not found"})
		}
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	eng := h.ensureEngine()
	if eng == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "campaign engine unavailable"})
	}

	camp, err := eng.Start(c.Request().Context(), scope, id)
	if err != nil {
		return handleRESTEngineError(c, err, "")
	}

	return c.JSON(http.StatusOK, camp)
}

// APIPause pauses an active campaign via REST API.
func (h *CampaignHandler) APIPause(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid campaign ID"})
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid campaign ID"})
	}

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "campaign not found"})
		}
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	eng := h.ensureEngine()
	if eng == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "campaign engine unavailable"})
	}

	camp, err := eng.Pause(c.Request().Context(), scope, id)
	if err != nil {
		return handleRESTEngineError(c, err, "failed to pause campaign")
	}

	return c.JSON(http.StatusOK, camp)
}

// APIResume resumes a paused campaign via REST API.
func (h *CampaignHandler) APIResume(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid campaign ID"})
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid campaign ID"})
	}

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "campaign not found"})
		}
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	eng := h.ensureEngine()
	if eng == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "campaign engine unavailable"})
	}

	camp, err := eng.Resume(c.Request().Context(), scope, id)
	if err != nil {
		return handleRESTEngineError(c, err, "failed to resume campaign")
	}

	return c.JSON(http.StatusOK, camp)
}

// APICancel cancels an active, paused, or scheduled campaign via REST API.
func (h *CampaignHandler) APICancel(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid campaign ID"})
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid campaign ID"})
	}

	scope, err := resolveScope(c)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "campaign not found"})
		}
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	eng := h.ensureEngine()
	if eng == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "campaign engine unavailable"})
	}

	camp, err := eng.Cancel(c.Request().Context(), scope, id)
	if err != nil {
		return handleRESTEngineError(c, err, "failed to cancel campaign")
	}

	return c.JSON(http.StatusOK, camp)
}
