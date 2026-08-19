package whatsapp

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/util/keys"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func TestWhatsAppClient_JIDMethods(t *testing.T) {
	parsedJID, err := types.ParseJID("5511999990001@s.whatsapp.net")
	if err != nil {
		t.Fatalf("failed to parse JID: %v", err)
	}

	deviceStore := &store.Device{
		ID: &parsedJID,
	}
	cli := whatsmeow.NewClient(deviceStore, waLog.Noop)

	wc := &WhatsAppClient{
		client: cli,
	}

	// Should read from client.Store.ID
	if wc.JID() != parsedJID {
		t.Errorf("expected JID %v, got %v", parsedJID, wc.JID())
	}

	// Update via SetJID
	newJID, _ := types.ParseJID("5511999990002@s.whatsapp.net")
	wc.SetJID(newJID)

	if wc.JID() != newJID {
		t.Errorf("expected updated JID %v, got %v", newJID, wc.JID())
	}
	if *deviceStore.ID != newJID {
		t.Errorf("expected deviceStore.ID to be synced to %v, got %v", newJID, *deviceStore.ID)
	}
}

func TestWhatsAppClient_FetchLatestVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ver, err := whatsmeow.GetLatestVersion(ctx, nil)
	if err != nil {
		t.Skipf("skipping live WhatsApp Web fetch: %v", err)
	}
	if ver == nil || (*ver)[0] == 0 {
		t.Fatalf("expected valid non-zero version container, got %v", ver)
	}
}

func TestWhatsAppClient_UpdateWAVersion_Explicit(t *testing.T) {
	ctx := context.Background()
	customVer := "2.3000.1045542031"
	res := UpdateWAVersion(ctx, customVer, nil)
	expected, _ := store.ParseVersion(customVer)

	if res != expected {
		t.Errorf("expected parsed version %v, got %v", expected, res)
	}

	cur := store.GetWAVersion()
	if cur != expected {
		t.Errorf("expected global WAVersion %v, got %v", expected, cur)
	}
}

func TestWhatsAppClient_UpdateWAVersion_Empty_AutoResolves(t *testing.T) {
	ctx := context.Background()
	res := UpdateWAVersion(ctx, "", nil)

	if res[0] == 0 {
		t.Errorf("expected major version > 0, got %v", res)
	}
}

func TestWhatsAppClient_DeviceStore_BinaryNoiseKeys(t *testing.T) {
	jid, _ := types.ParseJID("5511999998888@s.whatsapp.net")
	deviceStore := &store.Device{
		ID:           &jid,
		NoiseKey:     keys.NewKeyPair(),
		IdentityKey:  keys.NewKeyPair(),
		SignedPreKey: keys.NewPreKey(1),
	}

	if deviceStore.NoiseKey == nil || len(deviceStore.NoiseKey.Priv) == 0 {
		t.Fatalf("expected initialized noise key")
	}
}
