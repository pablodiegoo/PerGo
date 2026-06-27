package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ErrMediaSizeExceeded is returned when the downloaded file exceeds the maximum size boundary.
var ErrMediaSizeExceeded = errors.New("media_size_exceeded")

// S3Client wraps the AWS SDK v2 S3 client.
type S3Client struct {
	Client *s3.Client
	Bucket string
}

// DownloadResult holds metadata and data of the validated download.
type DownloadResult struct {
	Bytes       []byte
	Hash        string
	ContentType string
	Extension   string
}

// NewS3Client creates and configures a new S3Client.
func NewS3Client(endpoint, region, accessKey, secretKey, bucket string, usePathStyle bool) (*S3Client, error) {
	// Configure static credentials provider
	creds := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("load default config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = usePathStyle
	})

	return &S3Client{
		Client: client,
		Bucket: bucket,
	}, nil
}

// Upload stores bytes in S3.
func (s *S3Client) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("s3 put object: %w", err)
	}
	return nil
}

// Download retrieves file stream from S3.
func (s *S3Client) Download(ctx context.Context, key string) (io.ReadCloser, string, error) {
	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", err
	}

	contentType := ""
	if out.ContentType != nil {
		contentType = *out.ContentType
	}
	return out.Body, contentType, nil
}

// DownloadAndValidate downloads the media from source URL, enforcing size limits and timeouts.
func DownloadAndValidate(ctx context.Context, url string, maxBytes int64) (*DownloadResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received bad status code: %d", resp.StatusCode)
	}

	// Limit reader to limit reading up to maxBytes + 1
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
		// If the server didn't provide Content-Type or returned a generic octet-stream,
		// we prefer the sniffed MIME type.
		if contentType == "" || contentType == "application/octet-stream" {
			contentType = detected
		} else {
			// If sniffed MIME type is more specific than application/octet-stream, keep it
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
