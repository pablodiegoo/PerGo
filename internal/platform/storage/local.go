package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalDiskStorage implements MediaStorage backed by a persistent local filesystem directory.
// Objects are stored at {baseDir}/{key} with a sidecar {baseDir}/{key}.ct containing the MIME Content-Type.
type LocalDiskStorage struct {
	baseDir string
}

var _ MediaStorage = (*LocalDiskStorage)(nil)

// NewLocalDiskStorage creates a new LocalDiskStorage rooted at baseDir, creating the directory if needed.
func NewLocalDiskStorage(baseDir string) (*LocalDiskStorage, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return nil, errors.New("local disk storage baseDir cannot be empty")
	}
	absBase, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return nil, fmt.Errorf("resolve local disk storage baseDir: %w", err)
	}
	if err := os.MkdirAll(absBase, 0o755); err != nil {
		return nil, fmt.Errorf("create local disk storage baseDir %q: %w", absBase, err)
	}
	return &LocalDiskStorage{baseDir: absBase}, nil
}

// BaseDir returns the absolute root directory managed by this LocalDiskStorage.
func (s *LocalDiskStorage) BaseDir() string {
	if s == nil {
		return ""
	}
	return s.baseDir
}

func (s *LocalDiskStorage) resolveSafePath(key string) (string, error) {
	if s == nil || s.baseDir == "" {
		return "", errors.New("local disk storage is not initialized")
	}
	if key == "" || strings.ContainsRune(key, 0) || strings.Contains(key, "\\") || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("invalid storage key %q", key)
	}
	if strings.HasSuffix(strings.ToLower(key), ".ct") {
		return "", fmt.Errorf("reserved sidecar suffix in storage key %q", key)
	}

	parts := strings.Split(key, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("path traversal or empty segment rejected for key %q", key)
		}
	}

	fullPath := filepath.Join(s.baseDir, filepath.FromSlash(key))
	rel, err := filepath.Rel(s.baseDir, fullPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes baseDir for key %q", key)
	}

	return fullPath, nil
}

// Upload writes data and its Content-Type sidecar atomically under baseDir/{key}.
func (s *LocalDiskStorage) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	targetPath, err := s.resolveSafePath(key)
	if err != nil {
		return err
	}

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create object parent directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return fmt.Errorf("create temp media file: %w", err)
	}
	tmpName := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp media file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp media file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod temp media file: %w", err)
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("commit media file: %w", err)
	}
	cleanupTmp = false

	ct := strings.TrimSpace(contentType)
	if ct == "" {
		ct = "application/octet-stream"
	}
	if err := os.WriteFile(targetPath+".ct", []byte(ct), 0o644); err != nil {
		return fmt.Errorf("write content-type sidecar: %w", err)
	}

	return nil
}

// Download opens the stored file stream and reads its Content-Type sidecar.
// Returns ErrObjectNotFound when the key does not exist on disk.
func (s *LocalDiskStorage) Download(ctx context.Context, key string) (io.ReadCloser, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	targetPath, err := s.resolveSafePath(key)
	if err != nil {
		return nil, "", err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", ErrObjectNotFound
		}
		return nil, "", fmt.Errorf("stat media file: %w", err)
	}
	if info.IsDir() {
		return nil, "", ErrObjectNotFound
	}

	f, err := os.Open(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", ErrObjectNotFound
		}
		return nil, "", fmt.Errorf("open media file: %w", err)
	}

	contentType := "application/octet-stream"
	if ctBytes, ctErr := os.ReadFile(targetPath + ".ct"); ctErr == nil {
		if trimmed := strings.TrimSpace(string(ctBytes)); trimmed != "" {
			contentType = trimmed
		}
	}

	return f, contentType, nil
}

// Delete removes both the stored media file and its Content-Type sidecar.
func (s *LocalDiskStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	targetPath, err := s.resolveSafePath(key)
	if err != nil {
		return err
	}

	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete media file: %w", err)
	}
	if err := os.Remove(targetPath + ".ct"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete media sidecar: %w", err)
	}
	return nil
}
