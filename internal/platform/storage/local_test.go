package storage_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalDiskStorage_PersistenceAndLifecycle(t *testing.T) {
	baseDir := t.TempDir()
	ctx := context.Background()
	key := "a0000000-0000-0000-0000-000000000001/abc123def456.ogg"
	payload := []byte("OggS-persistent-voice-note-bytes")

	// Instance 1: Upload object
	store1, err := storage.NewLocalDiskStorage(baseDir)
	require.NoError(t, err)

	err = store1.Upload(ctx, key, payload, "audio/ogg; codecs=opus")
	require.NoError(t, err)

	// Instance 2: Simulate process restart reading from the same baseDir
	store2, err := storage.NewLocalDiskStorage(baseDir)
	require.NoError(t, err)

	rc, contentType, err := store2.Download(ctx, key)
	require.NoError(t, err)
	defer rc.Close()

	gotBytes, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, payload, gotBytes)
	assert.Equal(t, "audio/ogg; codecs=opus", contentType)

	// Delete object and confirm ErrObjectNotFound
	require.NoError(t, store2.Delete(ctx, key))

	_, _, err = store2.Download(ctx, key)
	require.ErrorIs(t, err, storage.ErrObjectNotFound)

	// Idempotent delete on missing object succeeds
	require.NoError(t, store2.Delete(ctx, key))
}

func TestLocalDiskStorage_MissingReturnsErrObjectNotFound(t *testing.T) {
	store, err := storage.NewLocalDiskStorage(t.TempDir())
	require.NoError(t, err)

	_, _, err = store.Download(context.Background(), "ws-1/nonexistent.png")
	require.True(t, errors.Is(err, storage.ErrObjectNotFound))
}

func TestLocalDiskStorage_BlocksPathTraversal(t *testing.T) {
	store, err := storage.NewLocalDiskStorage(t.TempDir())
	require.NoError(t, err)
	ctx := context.Background()

	maliciousKeys := []string{
		"",
		"../secret.txt",
		"ws/../../etc/passwd",
		"/etc/passwd",
		`ws\..\secret.txt`,
		"ws//double-slash.png",
		"ws/./dot.png",
		"ws/sidecar.ogg.ct",
		"ws/null\x00byte.ogg",
	}

	for _, badKey := range maliciousKeys {
		t.Run(badKey, func(t *testing.T) {
			err := store.Upload(ctx, badKey, []byte("x"), "text/plain")
			require.Error(t, err)

			_, _, err = store.Download(ctx, badKey)
			require.Error(t, err)

			err = store.Delete(ctx, badKey)
			require.Error(t, err)
		})
	}
}
