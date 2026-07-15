package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/platform/storage"
)

// maxMediaSize is the maximum allowed size of media files (25MB).
const maxMediaSize = 25 * 1024 * 1024

// ErrMediaSizeExceeded is returned when the downloaded file exceeds the maximum size boundary.
var ErrMediaSizeExceeded = errors.New("media_size_exceeded")

// DownloadResult holds metadata and data of the validated download.
type DownloadResult struct {
	Bytes       []byte
	Hash        string
	ContentType string
	Extension   string
}

// Downloader defines the seam for downloading remote media streams with safety checks.
type Downloader interface {
	Download(ctx context.Context, url string, headers map[string]string, maxBytes int64) (*DownloadResult, error)
}

// Uploader defines the seam for uploading media bytes to storage.
type Uploader interface {
	Upload(ctx context.Context, key string, data []byte, contentType string) error
}

// Engine combines downloader, uploader, and consolidated inbound/outbound processors.
type Engine interface {
	Downloader
	Uploader
	ProcessOutbound(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error)
	ProcessInbound(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error)
}

// DefaultEngine implements the Engine interface.
type DefaultEngine struct {
	s3Client *storage.S3Client
	client   *http.Client
}

// NewDefaultEngine creates a new DefaultEngine instance.
func NewDefaultEngine(s3Client *storage.S3Client) *DefaultEngine {
	return &DefaultEngine{
		s3Client: s3Client,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Download fetches media from the URL, enforcing headers, timeouts, and size limits.
func (e *DefaultEngine) Download(
	ctx context.Context,
	url string,
	headers map[string]string,
	maxBytes int64,
) (*DownloadResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received bad status code: %d", resp.StatusCode)
	}

	limitReader := io.LimitReader(resp.Body, maxBytes+1)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if int64(len(data)) > maxBytes {
		return nil, ErrMediaSizeExceeded
	}

	// Detect content type
	contentType := resp.Header.Get("Content-Type")
	if len(data) > 0 {
		detected := http.DetectContentType(data)
		if contentType == "" || contentType == "application/octet-stream" {
			contentType = detected
		} else {
			if detected != "application/octet-stream" {
				contentType = detected
			}
		}
	}

	// Calculate SHA-256 Hash
	hasher := sha256.New()
	hasher.Write(data)
	contentHash := hex.EncodeToString(hasher.Sum(nil))

	ext := mimeToExt(contentType)

	return &DownloadResult{
		Bytes:       data,
		Hash:        contentHash,
		ContentType: contentType,
		Extension:   ext,
	}, nil
}

// Upload stores media bytes in S3.
func (e *DefaultEngine) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	if e.s3Client == nil {
		return errors.New("s3 client is not configured")
	}
	return e.s3Client.Upload(ctx, key, data, contentType)
}

// ProcessOutbound downloads remote media, uploads it to S3, and returns a local proxy URL.
func (e *DefaultEngine) ProcessOutbound(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error) {
	res, err := e.Download(ctx, mediaURL, nil, maxMediaSize)
	if err != nil {
		return "", err
	}

	s3Key := fmt.Sprintf("%s/%s.%s", workspaceID.String(), res.Hash, res.Extension)
	err = e.Upload(ctx, s3Key, res.Bytes, res.ContentType)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("/media/%s/%s.%s", workspaceID.String(), res.Hash, res.Extension), nil
}

// ProcessInbound handles inbound media bytes, uploads them to S3, and returns a local proxy URL.
func (e *DefaultEngine) ProcessInbound(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error) {
	if int64(len(data)) > maxMediaSize {
		return "", ErrMediaSizeExceeded
	}

	hasher := sha256.New()
	hasher.Write(data)
	hashKey := hex.EncodeToString(hasher.Sum(nil))

	ext := getExtFromMediaType(mediaType)
	mimeType := getMimeFromMediaType(mediaType)
	s3Key := fmt.Sprintf("%s/%s.%s", workspaceID.String(), hashKey, ext)

	err := e.Upload(ctx, s3Key, data, mimeType)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("/media/%s/%s.%s", workspaceID.String(), hashKey, ext), nil
}

func getExtFromMediaType(mediaType string) string {
	switch mediaType {
	case "image":
		return "jpg"
	case "video":
		return "mp4"
	case "audio":
		return "ogg"
	case "document":
		return "pdf"
	default:
		return "bin"
	}
}

func getMimeFromMediaType(mediaType string) string {
	switch mediaType {
	case "image":
		return "image/jpeg"
	case "video":
		return "video/mp4"
	case "audio":
		return "audio/ogg"
	case "document":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

func mimeToExt(mime string) string {
	if idx := strings.Index(mime, ";"); idx != -1 {
		mime = mime[:idx]
	}
	mime = strings.TrimSpace(mime)
	switch mime {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "video/mp4":
		return "mp4"
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/ogg":
		return "ogg"
	case "application/pdf":
		return "pdf"
	default:
		return "bin"
	}
}
