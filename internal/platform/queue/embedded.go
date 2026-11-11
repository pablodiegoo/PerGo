package queue

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// StartEmbeddedNATS starts an in-process NATS server with JetStream enabled,
// storing state in storeDir and listening on 127.0.0.1:port (use -1 for an ephemeral port, 0 for default 4222).
// It waits up to 10 seconds for the server to accept client connections and returns the server and client URL.
func StartEmbeddedNATS(storeDir string, port int) (*server.Server, string, error) {
	if storeDir == "" {
		storeDir = "./data/nats"
	}
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create embedded nats store dir %q: %w", storeDir, err)
	}

	opts := &server.Options{
		Host:      "127.0.0.1",
		Port:      port,
		JetStream: true,
		StoreDir:  storeDir,
		NoSigs:    true,
		NoLog:     true,
	}

	ns, err := server.NewServer(opts)
	if err != nil {
		return nil, "", fmt.Errorf("create embedded nats server: %w", err)
	}

	go ns.Start()

	if !ns.ReadyForConnections(10 * time.Second) {
		ns.Shutdown()
		return nil, "", errors.New("embedded nats server failed to become ready within 10s")
	}

	return ns, ns.ClientURL(), nil
}
