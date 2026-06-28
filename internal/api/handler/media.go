package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/platform/storage"
)

// MediaHandler serves stored S3 objects securely.
type MediaHandler struct {
	S3Client *storage.S3Client
}

// NewMediaHandler creates a new MediaHandler.
func NewMediaHandler(s3Client *storage.S3Client) *MediaHandler {
	return &MediaHandler{
		S3Client: s3Client,
	}
}

// Handle streams stored bytes from S3.
func (h *MediaHandler) Handle(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	// Validate context workspace_id matches path parameter to enforce security boundary.
	ctxWorkspaceID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok || ctxWorkspaceID != workspaceID {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	}

	hashWithExt, err := echo.PathParam[string](c, "hash")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid hash")
	}

	// The key format in S3 is {workspace_id}/{hash}
	key := workspaceID.String() + "/" + hashWithExt

	body, contentType, err := h.S3Client.Download(c.Request().Context(), key)
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return c.String(http.StatusNotFound, "media not found")
		}
		return c.String(http.StatusInternalServerError, "failed to download media")
	}
	defer body.Close()

	if contentType != "" {
		c.Response().Header().Set(echo.HeaderContentType, contentType)
	}

	c.Response().WriteHeader(http.StatusOK)
	_, err = io.Copy(c.Response(), body)
	return err
}
