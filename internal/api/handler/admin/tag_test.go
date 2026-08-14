package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestTagAdminHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "tag_admin_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	handler := admin.NewTagAdminHandler(tagRepo, contactRepo, wsRepo)

	var createdTag domain.Tag

	t.Run("CreateTag_Success", func(t *testing.T) {
		e := echo.New()
		body := `{"name": "VIP Customer", "color": "#FF0000"}`
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/tags", ws.ID), strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/workspaces/:workspace_id/tags")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		if err := handler.CreateTag(c); err != nil {
			t.Fatalf("CreateTag failed: %v", err)
		}

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}

		if err := json.Unmarshal(rec.Body.Bytes(), &createdTag); err != nil {
			t.Fatalf("failed to unmarshal created tag: %v", err)
		}
		if createdTag.Name != "VIP Customer" || createdTag.Color != "#FF0000" {
			t.Errorf("unexpected created tag: %+v", createdTag)
		}
	})

	t.Run("ListTags_Success", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/tags", ws.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/workspaces/:workspace_id/tags")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		if err := handler.ListTags(c); err != nil {
			t.Fatalf("ListTags failed: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var tags []domain.Tag
		if err := json.Unmarshal(rec.Body.Bytes(), &tags); err != nil {
			t.Fatalf("failed to unmarshal tags list: %v", err)
		}
		if len(tags) != 1 || tags[0].ID != createdTag.ID {
			t.Errorf("expected 1 tag, got %+v", tags)
		}
	})

	t.Run("Add_And_Remove_ContactTag_Success", func(t *testing.T) {
		contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "5511888887777", "Alice Handler Test", "", "")
		if err != nil {
			t.Fatalf("failed to resolve contact: %v", err)
		}

		e := echo.New()

		// Add tag
		reqAdd := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/contacts/%s/tags/%s", ws.ID, contact.ID, createdTag.ID), nil)
		recAdd := httptest.NewRecorder()
		cAdd := e.NewContext(reqAdd, recAdd)
		cAdd.SetPath("/api/v1/workspaces/:workspace_id/contacts/:contact_id/tags/:tag_id")
		cAdd.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
			{Name: "contact_id", Value: contact.ID.String()},
			{Name: "tag_id", Value: createdTag.ID.String()},
		})

		if err := handler.AddContactTag(cAdd); err != nil {
			t.Fatalf("AddContactTag failed: %v", err)
		}
		if recAdd.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for AddContactTag, got %d: %s", recAdd.Code, recAdd.Body.String())
		}

		// Remove tag
		reqRemove := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/%s/contacts/%s/tags/%s", ws.ID, contact.ID, createdTag.ID), nil)
		recRemove := httptest.NewRecorder()
		cRemove := e.NewContext(reqRemove, recRemove)
		cRemove.SetPath("/api/v1/workspaces/:workspace_id/contacts/:contact_id/tags/:tag_id")
		cRemove.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
			{Name: "contact_id", Value: contact.ID.String()},
			{Name: "tag_id", Value: createdTag.ID.String()},
		})

		if err := handler.RemoveContactTag(cRemove); err != nil {
			t.Fatalf("RemoveContactTag failed: %v", err)
		}
		if recRemove.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for RemoveContactTag, got %d: %s", recRemove.Code, recRemove.Body.String())
		}
	})

	t.Run("ImportContactsCSV_Success", func(t *testing.T) {
		e := echo.New()

		csvData := "name,phone,channel,email,city,plan,tags\nBob CSV,5511911112222,whatsapp,bob@example.com,Belo Horizonte,Enterprise,Lead,High-Value\n"
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "contacts.csv")
		if err != nil {
			t.Fatalf("failed to create form file: %v", err)
		}
		_, _ = part.Write([]byte(csvData))
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/contacts/import", ws.ID), body)
		req.Header.Set(echo.HeaderContentType, writer.FormDataContentType())
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/workspaces/:workspace_id/contacts/import")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		if err := handler.ImportContactsCSV(c); err != nil {
			t.Fatalf("ImportContactsCSV failed: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var res admin.ImportResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to unmarshal import result: %v", err)
		}
		if res.Imported != 1 || res.Errors != 0 {
			t.Errorf("unexpected import result: %+v", res)
		}

		// Verify contact custom attributes
		searched, err := contactRepo.SearchContacts(context.Background(), ws.ID, "Bob CSV", uuid.Nil, 10)
		if err != nil || len(searched) == 0 {
			t.Fatalf("failed to find imported contact: %v", err)
		}
		if searched[0].Attributes["city"] != "Belo Horizonte" || searched[0].Attributes["plan"] != "Enterprise" {
			t.Errorf("expected imported custom attributes city=Belo Horizonte, plan=Enterprise, got %v", searched[0].Attributes)
		}
	})

	t.Run("ExportContactsCSV_Success", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/contacts/export", ws.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/workspaces/:workspace_id/contacts/export")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		if err := handler.ExportContactsCSV(c); err != nil {
			t.Fatalf("ExportContactsCSV failed: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "id,name,email,channel,sender_identity,tags,created_at") {
			t.Errorf("expected CSV header in body, got %s", rec.Body.String())
		}
	})

	t.Run("DeleteTag_Success", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/%s/tags/%s", ws.ID, createdTag.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/workspaces/:workspace_id/tags/:id")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
			{Name: "id", Value: createdTag.ID.String()},
		})

		if err := handler.DeleteTag(c); err != nil {
			t.Fatalf("DeleteTag failed: %v", err)
		}

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 244 No Content, got %d: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("RedirectToWorkspaceTags_Success", func(t *testing.T) {
		handlerWithWS := admin.NewTagAdminHandler(tagRepo, contactRepo, wsRepo)
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/tags", nil)
		req.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := handlerWithWS.RedirectToWorkspaceTags(c); err != nil {
			t.Fatalf("RedirectToWorkspaceTags failed: %v", err)
		}

		if rec.Code != http.StatusFound {
			t.Fatalf("expected 302 Found, got %d", rec.Code)
		}
		expectedLocation := fmt.Sprintf("/admin/workspaces/%s/tags", ws.ID)
		if loc := rec.Header().Get("Location"); loc != expectedLocation {
			t.Errorf("expected Location %s, got %s", expectedLocation, loc)
		}
	})
}
