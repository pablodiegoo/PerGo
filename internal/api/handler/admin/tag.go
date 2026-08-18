package admin

import (
	"encoding/csv"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

type TagAdminHandler struct {
	tagRepo     *repository.TagRepository
	contactRepo *repository.ContactRepository
	wsRepo      *repository.WorkspaceRepository
}

func NewTagAdminHandler(tagRepo *repository.TagRepository, contactRepo *repository.ContactRepository, wsRepo *repository.WorkspaceRepository) *TagAdminHandler {
	return &TagAdminHandler{
		tagRepo:     tagRepo,
		contactRepo: contactRepo,
		wsRepo:      wsRepo,
	}
}

// RedirectToWorkspaceTags handles legacy GET /tags by redirecting to /admin/tags
func (h *TagAdminHandler) RedirectToWorkspaceTags(c *echo.Context) error {
	return c.Redirect(http.StatusFound, "/admin/tags")
}

type CreateTagRequest struct {
	Name  string `json:"name" form:"name"`
	Color string `json:"color" form:"color"`
}

// ListTags handles GET /api/v1/workspaces/:workspace_id/tags or GET /admin/tags
// Page handles GET /admin/tags or legacy GET /admin/workspaces/:workspace_id/tags
func (h *TagAdminHandler) Page(c *echo.Context) error {
	workspaceID, err := resolveWorkspaceID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	tags, err := h.tagRepo.ListTags(c.Request().Context(), workspaceID)
	if err != nil {
		tags = []domain.Tag{}
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.TagListFragment(workspaceID, tags))
	}
	return mw.Render(c, http.StatusOK, pages.TagsPage(workspaceID, tags))
}

func (h *TagAdminHandler) ListTags(c *echo.Context) error {
	workspaceID, err := resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	tags, err := h.tagRepo.ListTags(c.Request().Context(), workspaceID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, tags)
}

// CreateTag handles POST /api/v1/workspaces/:workspace_id/tags or POST /admin/tags
func (h *TagAdminHandler) CreateTag(c *echo.Context) error {
	workspaceID, err := resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	var req CreateTagRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	tag, err := h.tagRepo.CreateTag(c.Request().Context(), workspaceID, req.Name, req.Color)
	if err != nil {
		if errors.Is(err, repository.ErrTagAlreadyExists) {
			return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.TagRow(workspaceID, *tag))
	}

	return c.JSON(http.StatusCreated, tag)
}

// DeleteTag handles DELETE /api/v1/workspaces/:workspace_id/tags/:id or DELETE /admin/tags/:id
func (h *TagAdminHandler) DeleteTag(c *echo.Context) error {
	workspaceID, err := resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	tagIDStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tag ID"})
	}
	tagID, err := uuid.Parse(tagIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tag ID"})
	}

	if err := h.tagRepo.DeleteTag(c.Request().Context(), workspaceID, tagID); err != nil {
		if errors.Is(err, repository.ErrTagNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}

// AddContactTag handles POST /api/v1/workspaces/:workspace_id/contacts/:contact_id/tags/:tag_id
func (h *TagAdminHandler) AddContactTag(c *echo.Context) error {
	workspaceID, err := resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	contactIDStr, err := echo.PathParam[string](c, "contact_id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid contact ID"})
	}
	contactID, err := uuid.Parse(contactIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid contact ID"})
	}

	tagIDStr, err := echo.PathParam[string](c, "tag_id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tag ID"})
	}
	tagID, err := uuid.Parse(tagIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tag ID"})
	}

	if err := h.tagRepo.AddTagToContact(c.Request().Context(), workspaceID, contactID, tagID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "tag_added"})
}

// RemoveContactTag handles DELETE /api/v1/workspaces/:workspace_id/contacts/:contact_id/tags/:tag_id
func (h *TagAdminHandler) RemoveContactTag(c *echo.Context) error {
	workspaceID, err := resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	contactIDStr, err := echo.PathParam[string](c, "contact_id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid contact ID"})
	}
	contactID, err := uuid.Parse(contactIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid contact ID"})
	}

	tagIDStr, err := echo.PathParam[string](c, "tag_id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tag ID"})
	}
	tagID, err := uuid.Parse(tagIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tag ID"})
	}

	if err := h.tagRepo.RemoveTagFromContact(c.Request().Context(), workspaceID, contactID, tagID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "tag_removed"})
}

type ImportResult struct {
	TotalProcessed int `json:"total_processed"`
	Imported       int `json:"imported"`
	Errors         int `json:"errors"`
}

// ImportContactsCSV handles POST /api/v1/workspaces/:workspace_id/contacts/import or POST /admin/contacts/import
func (h *TagAdminHandler) ImportContactsCSV(c *echo.Context) error {
	workspaceID, err := resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "csv file is required"})
	}

	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to open csv file"})
	}
	defer src.Close()

	reader := csv.NewReader(src)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1 // Allow variable fields per record

	headers, err := reader.Read()
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid csv format or empty file"})
	}

	// Map header indices
	nameIdx, phoneIdx, channelIdx, emailIdx, tagsIdx := -1, -1, -1, -1, -1
	for i, h := range headers {
		lower := strings.ToLower(strings.TrimSpace(h))
		switch lower {
		case "name", "nome":
			nameIdx = i
		case "phone", "telefone", "sender_identity", "identity":
			phoneIdx = i
		case "channel", "canal":
			channelIdx = i
		case "email":
			emailIdx = i
		case "tags", "etiquetas":
			tagsIdx = i
		}
	}

	if phoneIdx == -1 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "csv must contain a 'phone' or 'sender_identity' column"})
	}

	result := ImportResult{}
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Errors++
			continue
		}
		result.TotalProcessed++

		if phoneIdx >= len(record) {
			result.Errors++
			continue
		}

		senderIdentity := strings.TrimSpace(record[phoneIdx])
		if senderIdentity == "" {
			result.Errors++
			continue
		}

		name := senderIdentity
		if nameIdx != -1 && nameIdx < len(record) && strings.TrimSpace(record[nameIdx]) != "" {
			name = strings.TrimSpace(record[nameIdx])
		}

		channel := "whatsapp"
		if channelIdx != -1 && channelIdx < len(record) && strings.TrimSpace(record[channelIdx]) != "" {
			channel = strings.ToLower(strings.TrimSpace(record[channelIdx]))
		}

		email := ""
		if emailIdx != -1 && emailIdx < len(record) {
			email = strings.TrimSpace(record[emailIdx])
		}

		contact, err := h.contactRepo.ResolveContact(c.Request().Context(), workspaceID, channel, senderIdentity, name, "", email)
		if err != nil {
			result.Errors++
			continue
		}

		// Handle tags column if present (collect all fields starting from tagsIdx)
		if tagsIdx != -1 && tagsIdx < len(record) {
			rawTagsStr := strings.Join(record[tagsIdx:], ",")
			rawTags := strings.Split(rawTagsStr, ",")
			for _, tagStr := range rawTags {
				tagName := strings.TrimSpace(tagStr)
				if tagName == "" {
					continue
				}
				tag, err := h.tagRepo.CreateTag(c.Request().Context(), workspaceID, tagName, "#6B7280")
				if err != nil && !errors.Is(err, repository.ErrTagAlreadyExists) {
					continue
				}
				if errors.Is(err, repository.ErrTagAlreadyExists) {
					tags, _ := h.tagRepo.ListTags(c.Request().Context(), workspaceID)
					for _, t := range tags {
						if strings.EqualFold(t.Name, tagName) {
							tag = &t
							break
						}
					}
				}
				if tag != nil {
					_ = h.tagRepo.AddTagToContact(c.Request().Context(), workspaceID, contact.ID, tag.ID)
				}
			}
		}

		// Collect custom attributes from unmapped columns (excluding standard fields and tags)
		attrs := make(map[string]string)
		for i, h := range headers {
			if i == nameIdx || i == phoneIdx || i == channelIdx || i == emailIdx || (tagsIdx != -1 && i >= tagsIdx) {
				continue
			}
			if i < len(record) {
				val := strings.TrimSpace(record[i])
				if val != "" {
					key := strings.ToLower(strings.TrimSpace(h))
					attrs[key] = val
				}
			}
		}
		if len(attrs) > 0 {
			if err := h.contactRepo.UpdateAttributes(c.Request().Context(), workspaceID, contact.ID, attrs); err != nil {
				slog.Error("failed to update contact attributes during csv import", "workspace_id", workspaceID, "contact_id", contact.ID, "error", err)
			}
		}

		result.Imported++
	}

	return c.JSON(http.StatusOK, result)
}



// ExportContactsCSV handles GET /api/v1/workspaces/:workspace_id/contacts/export or GET /admin/contacts/export
func (h *TagAdminHandler) ExportContactsCSV(c *echo.Context) error {
	workspaceID, err := resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	tagIDStr := c.QueryParam("tag_id")
	var contacts []domain.Contact
	if tagIDStr != "" {
		tagID, err := uuid.Parse(tagIDStr)
		if err == nil {
			var tagErr error
			contacts, tagErr = h.tagRepo.ListContactsByTag(c.Request().Context(), workspaceID, tagID)
			if tagErr != nil {
				slog.Error("failed to list contacts by tag for export", "workspace_id", workspaceID, "tag_id", tagID, "error", tagErr)
			}
		}
	}
	if contacts == nil {
		var searchErr error
		contacts, searchErr = h.contactRepo.SearchContacts(c.Request().Context(), workspaceID, "", uuid.Nil, 10000)
		if searchErr != nil {
			slog.Error("failed to search contacts for export", "workspace_id", workspaceID, "error", searchErr)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": searchErr.Error()})
		}
	}

	c.Response().Header().Set("Content-Type", "text/csv")
	c.Response().Header().Set("Content-Disposition", "attachment; filename=contacts.csv")

	return domain.WriteContactsCSV(c.Response(), contacts)
}

