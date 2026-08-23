package handler_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

type mockMediaStorage struct {
	downloadFunc func(ctx context.Context, key string) (io.ReadCloser, string, error)
}

func (m *mockMediaStorage) Download(ctx context.Context, key string) (io.ReadCloser, string, error) {
	return m.downloadFunc(ctx, key)
}

func TestMediaHandler_Handle(t *testing.T) {
	wsID := uuid.New()
	hash := "sample123.ogg"

	t.Run("Valid request streams media with content-type", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/media/"+wsID.String()+"/"+hash, nil)
		req = req.WithContext(tenant.WithWorkspaceID(req.Context(), wsID))
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/media/:workspace_id/:hash")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: wsID.String()},
			{Name: "hash", Value: hash},
		})

		mockStore := &mockMediaStorage{
			downloadFunc: func(ctx context.Context, key string) (io.ReadCloser, string, error) {
				assert.Equal(t, wsID.String()+"/"+hash, key)
				return io.NopCloser(bytes.NewReader([]byte("AUDIO_DATA_BYTES"))), "audio/ogg", nil
			},
		}

		h := handler.NewMediaHandler(mockStore)
		err := h.Handle(c)
		require.NoError(t, err)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "audio/ogg", rec.Header().Get("Content-Type"))
		assert.Equal(t, "AUDIO_DATA_BYTES", rec.Body.String())
	})

	t.Run("Mismatched tenant in context returns 403 Forbidden", func(t *testing.T) {
		otherWsID := uuid.New()
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/media/"+wsID.String()+"/"+hash, nil)
		// Context belongs to otherWsID, but request is asking for wsID
		req = req.WithContext(tenant.WithWorkspaceID(req.Context(), otherWsID))
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/media/:workspace_id/:hash")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: wsID.String()},
			{Name: "hash", Value: hash},
		})

		mockStore := &mockMediaStorage{}
		h := handler.NewMediaHandler(mockStore)
		err := h.Handle(c)
		require.NoError(t, err)

		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Body.String(), "forbidden")
	})

	t.Run("Missing context tenant returns 403 Forbidden", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/media/"+wsID.String()+"/"+hash, nil)
		// No tenant in context
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/media/:workspace_id/:hash")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: wsID.String()},
			{Name: "hash", Value: hash},
		})

		mockStore := &mockMediaStorage{}
		h := handler.NewMediaHandler(mockStore)
		err := h.Handle(c)
		require.NoError(t, err)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("Non-existent S3 key returns 404 Not Found", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/media/"+wsID.String()+"/"+hash, nil)
		req = req.WithContext(tenant.WithWorkspaceID(req.Context(), wsID))
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/media/:workspace_id/:hash")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: wsID.String()},
			{Name: "hash", Value: hash},
		})

		mockStore := &mockMediaStorage{
			downloadFunc: func(ctx context.Context, key string) (io.ReadCloser, string, error) {
				return nil, "", &types.NoSuchKey{}
			},
		}

		h := handler.NewMediaHandler(mockStore)
		err := h.Handle(c)
		require.NoError(t, err)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Contains(t, rec.Body.String(), "media not found")
	})

	t.Run("S3 download error returns 500 InternalServerError", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/media/"+wsID.String()+"/"+hash, nil)
		req = req.WithContext(tenant.WithWorkspaceID(req.Context(), wsID))
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/media/:workspace_id/:hash")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: wsID.String()},
			{Name: "hash", Value: hash},
		})

		mockStore := &mockMediaStorage{
			downloadFunc: func(ctx context.Context, key string) (io.ReadCloser, string, error) {
				return nil, "", errors.New("s3 internal timeout")
			},
		}

		h := handler.NewMediaHandler(mockStore)
		err := h.Handle(c)
		require.NoError(t, err)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "failed to download media")
	})

	t.Run("Invalid workspace ID format returns 400 BadRequest", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/media/invalid-uuid/"+hash, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/media/:workspace_id/:hash")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: "invalid-uuid"},
			{Name: "hash", Value: hash},
		})

		mockStore := &mockMediaStorage{}
		h := handler.NewMediaHandler(mockStore)
		err := h.Handle(c)
		require.NoError(t, err)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid workspace ID")
	})
}
