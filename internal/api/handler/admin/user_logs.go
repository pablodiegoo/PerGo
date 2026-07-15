package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

// UserLogsHandler handles requests related to administrative action logs.
type UserLogsHandler struct {
	Repo *repository.UserActionLogRepository
}

// NewUserLogsHandler creates a new UserLogsHandler instance.
func NewUserLogsHandler(repo *repository.UserActionLogRepository) *UserLogsHandler {
	return &UserLogsHandler{Repo: repo}
}

// List renders the user action logs page.
// GET /admin/user-logs
func (h *UserLogsHandler) List(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaceID := resolveWorkspaceID(c)
	if workspaceID == uuid.Nil {
		return c.Redirect(http.StatusFound, "/admin/")
	}

	limit := 50
	offset := 0
	if offsetStr := c.QueryParam("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	actorTypeFilter := c.QueryParam("actor_type")
	sourceFilter := c.QueryParam("source")

	logs, total, err := h.Repo.ListByWorkspace(ctx, workspaceID, limit, offset, actorTypeFilter, sourceFilter)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load action logs: "+err.Error())
	}

	return mw.Render(c, http.StatusOK, pages.UserLogsPage(workspaceID, logs, limit, offset, total, actorTypeFilter, sourceFilter))
}

// GetMetadata renders the modal fragment showing formatted metadata.
// GET /admin/user-logs/:id/metadata
func (h *UserLogsHandler) GetMetadata(c *echo.Context) error {
	ctx := c.Request().Context()
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil || idStr == "" {
		return c.String(http.StatusBadRequest, "missing log id")
	}

	logID, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid log id")
	}

	log, err := h.Repo.GetByID(ctx, logID)
	if err != nil {
		return c.String(http.StatusNotFound, "log not found")
	}

	// Format metadata as indented JSON for display
	var formattedJSON string
	if len(log.Metadata) > 0 {
		var js any
		if json.Unmarshal(log.Metadata, &js) == nil {
			indentBytes, err := json.MarshalIndent(js, "", "  ")
			if err == nil {
				formattedJSON = string(indentBytes)
			}
		}
	}
	if formattedJSON == "" {
		formattedJSON = "{}"
	}

	return mw.Render(c, http.StatusOK, pages.UserLogMetadataModal(*log, formattedJSON))
}
