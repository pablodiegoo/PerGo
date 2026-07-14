package whatsapp

import (
	"io"
	"net"
	"testing"
	"time"

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

func TestConfigureProxy_Integration(t *testing.T) {
	expectedUser := "testuser"
	expectedPass := "testpass"

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer l.Close()

	proxyAddr := l.Addr().String()
	authChan := make(chan bool, 1)

	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 256)
		if _, err := io.ReadFull(conn, buf[:2]); err != nil {
			t.Errorf("proxy: failed to read greeting: %v", err)
			return
		}

		if buf[0] != 0x05 {
			t.Errorf("proxy: unexpected version %d", buf[0])
			return
		}

		nmethods := int(buf[1])
		methods := make([]byte, nmethods)
		if _, err := io.ReadFull(conn, methods); err != nil {
			t.Errorf("proxy: failed to read methods: %v", err)
			return
		}

		hasUserPass := false
		for _, m := range methods {
			if m == 0x02 {
				hasUserPass = true
				break
			}
		}

		if !hasUserPass {
			conn.Write([]byte{0x05, 0xff})
			t.Errorf("proxy: user/pass auth method not proposed")
			return
		}

		// Respond with username/password method (0x02)
		conn.Write([]byte{0x05, 0x02})

		// Read auth request
		if _, err := io.ReadFull(conn, buf[:2]); err != nil {
			t.Errorf("proxy: failed to read auth header: %v", err)
			return
		}

		if buf[0] != 0x01 {
			t.Errorf("proxy: unexpected subnegotiation version %d", buf[0])
			return
		}

		userLen := int(buf[1])
		userBuf := make([]byte, userLen)
		if _, err := io.ReadFull(conn, userBuf); err != nil {
			t.Errorf("proxy: failed to read username: %v", err)
			return
		}

		// Read password
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			t.Errorf("proxy: failed to read password length: %v", err)
			return
		}
		passLen := int(buf[0])
		passBuf := make([]byte, passLen)
		if _, err := io.ReadFull(conn, passBuf); err != nil {
			t.Errorf("proxy: failed to read password: %v", err)
			return
		}

		if string(userBuf) == expectedUser && string(passBuf) == expectedPass {
			conn.Write([]byte{0x01, 0x00}) // auth success
			authChan <- true
		} else {
			conn.Write([]byte{0x01, 0x01}) // auth failure
			authChan <- false
		}
	}()

	deviceStore := &store.Device{}
	cli := whatsmeow.NewClient(deviceStore, waLog.Noop)

	proxyURL := "socks5://" + expectedUser + ":" + expectedPass + "@" + proxyAddr
	err = ConfigureProxy(cli, proxyURL)
	if err != nil {
		t.Fatalf("failed to configure proxy: %v", err)
	}

	// Trigger Connect. Since the proxy doesn't actually forward traffic to WhatsApp,
	// Connect will fail or timeout, which is expected. We just want to check if the
	// handshake is authenticated correctly.
	_ = cli.Connect()

	// Wait for proxy auth result
	select {
	case success := <-authChan:
		if !success {
			t.Error("proxy authentication failed (incorrect user/pass sent by whatsmeow)")
		}
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for proxy authentication")
	}
}
