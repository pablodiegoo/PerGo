package queue

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartEmbeddedNATS(t *testing.T) {
	storeDir := t.TempDir()

	ns, clientURL, err := StartEmbeddedNATS(storeDir, -1)
	require.NoError(t, err)
	require.NotNil(t, ns)
	require.NotEmpty(t, clientURL)
	defer func() {
		ns.Shutdown()
		ns.WaitForShutdown()
	}()

	nc, err := nats.Connect(clientURL)
	require.NoError(t, err)
	defer nc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := EnsureStream(ctx, nc)
	require.NoError(t, err)
	require.NotNil(t, stream)

	pub := NewJetStreamPublisher(nc)
	err = pub.Publish(ctx, StreamSubject, []byte(`{"hello":"solo"}`), "trace-embedded-1")
	require.NoError(t, err)

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	info, err := js.Stream(ctx, StreamName)
	require.NoError(t, err)
	stInfo, err := info.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), stInfo.State.Msgs)
}
