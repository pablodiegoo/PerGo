package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func TestConfigureProxy(t *testing.T) {
	// Create a dummy device store
	deviceStore := &store.Device{}
	cli := whatsmeow.NewClient(deviceStore, waLog.Noop)

	tests := []struct {
		name      string
		proxyStr  string
		expectErr bool
	}{
		{
			name:      "empty proxy",
			proxyStr:  "",
			expectErr: false,
		},
		{
			name:      "http proxy",
			proxyStr:  "http://localhost:8080",
			expectErr: false,
		},
		{
			name:      "socks5 proxy without credentials",
			proxyStr:  "socks5://localhost:1080",
			expectErr: false,
		},
		{
			name:      "socks5 proxy with credentials",
			proxyStr:  "socks5://user:pass@localhost:1080",
			expectErr: false,
		},
		{
			name:      "unsupported scheme",
			proxyStr:  "ftp://localhost:21",
			expectErr: true,
		},
		{
			name:      "invalid url",
			proxyStr:  "://invalid",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ConfigureProxy(cli, tt.proxyStr)
			if (err != nil) != tt.expectErr {
				t.Errorf("ConfigureProxy() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}
