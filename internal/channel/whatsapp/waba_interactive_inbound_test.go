package whatsapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestWABAInboundAdapter_InteractiveReplies(t *testing.T) {
	ctx := context.Background()
	adapter := NewWABAInboundAdapter(nil)

	wsID := uuid.New()
	connID := uuid.New()

	// Generate RSA keypair for testing encrypted flows
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	privKeyBytes := x509.MarshalPKCS1PrivateKey(rsaKey)
	privKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privKeyBytes,
	})

	creds := map[string]interface{}{
		"verify_token": "test_verify_token",
		"token":        "test_token",
		"private_key":  string(privKeyPEM),
	}
	credsJSON, _ := json.Marshal(creds)

	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		SenderIdentity: "+5511999990000",
		Credentials:    credsJSON,
	}

	t.Run("button_reply parsing", func(t *testing.T) {
		payload := []byte(`{
			"object": "whatsapp_business_account",
			"entry": [{
				"id": "12345",
				"changes": [{
					"field": "messages",
					"value": {
						"messaging_product": "whatsapp",
						"metadata": {
							"display_phone_number": "5511999990000",
							"phone_number_id": "phone_123"
						},
						"messages": [{
							"from": "5511988887777",
							"id": "wamid.btn_reply_001",
							"timestamp": "1700000000",
							"type": "interactive",
							"interactive": {
								"type": "button_reply",
								"button_reply": {
									"id": "btn_yes",
									"title": "Yes, I agree"
								}
							}
						}]
					}
				}]
			}]
		}`)

		events, err := adapter.Parse(ctx, payload, nil, conn)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		ev := events[0]
		if ev.Body != "🔘 *Selected*: Yes, I agree" {
			t.Errorf("expected Body '🔘 *Selected*: Yes, I agree', got %q", ev.Body)
		}
		if ev.Interactive == nil {
			t.Fatalf("expected ev.Interactive to be non-nil")
		}
		if ev.Interactive.Type != "button_reply" {
			t.Errorf("expected interactive type 'button_reply', got %q", ev.Interactive.Type)
		}
		if ev.Interactive.ButtonReply == nil {
			t.Fatalf("expected ButtonReply to be non-nil")
		}
		if ev.Interactive.ButtonReply.ID != "btn_yes" || ev.Interactive.ButtonReply.Title != "Yes, I agree" {
			t.Errorf("unexpected ButtonReply: %+v", ev.Interactive.ButtonReply)
		}
	})

	t.Run("legacy button message parsing", func(t *testing.T) {
		payload := []byte(`{
			"object": "whatsapp_business_account",
			"entry": [{
				"id": "12345",
				"changes": [{
					"field": "messages",
					"value": {
						"messaging_product": "whatsapp",
						"metadata": {
							"display_phone_number": "5511999990000",
							"phone_number_id": "phone_123"
						},
						"messages": [{
							"from": "5511988887777",
							"id": "wamid.btn_legacy_001",
							"timestamp": "1700000000",
							"type": "button",
							"button": {
								"payload": "btn_payload_xyz",
								"text": "Confirm Option"
							}
						}]
					}
				}]
			}]
		}`)

		events, err := adapter.Parse(ctx, payload, nil, conn)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		ev := events[0]
		if ev.Body != "🔘 *Selected*: Confirm Option" {
			t.Errorf("expected Body '🔘 *Selected*: Confirm Option', got %q", ev.Body)
		}
		if ev.Interactive == nil {
			t.Fatalf("expected ev.Interactive to be non-nil")
		}
		if ev.Interactive.Type != "button_reply" {
			t.Errorf("expected interactive type 'button_reply', got %q", ev.Interactive.Type)
		}
		if ev.Interactive.ButtonReply == nil {
			t.Fatalf("expected ButtonReply to be non-nil")
		}
		if ev.Interactive.ButtonReply.ID != "btn_payload_xyz" || ev.Interactive.ButtonReply.Title != "Confirm Option" {
			t.Errorf("unexpected ButtonReply: %+v", ev.Interactive.ButtonReply)
		}
	})

	t.Run("list_reply parsing with description", func(t *testing.T) {
		payload := []byte(`{
			"object": "whatsapp_business_account",
			"entry": [{
				"id": "12345",
				"changes": [{
					"field": "messages",
					"value": {
						"messaging_product": "whatsapp",
						"metadata": {
							"display_phone_number": "5511999990000",
							"phone_number_id": "phone_123"
						},
						"messages": [{
							"from": "5511988887777",
							"id": "wamid.list_reply_001",
							"timestamp": "1700000000",
							"type": "interactive",
							"interactive": {
								"type": "list_reply",
								"list_reply": {
									"id": "item_123",
									"title": "Premium Support Plan",
									"description": "24/7 dedicated telephone and chat support"
								}
							}
						}]
					}
				}]
			}]
		}`)

		events, err := adapter.Parse(ctx, payload, nil, conn)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		ev := events[0]
		expectedBody := "🔘 *Selected*: Premium Support Plan\n24/7 dedicated telephone and chat support"
		if ev.Body != expectedBody {
			t.Errorf("expected Body %q, got %q", expectedBody, ev.Body)
		}
		if ev.Interactive == nil || ev.Interactive.Type != "list_reply" {
			t.Fatalf("expected interactive list_reply, got %+v", ev.Interactive)
		}
		if ev.Interactive.ListReply == nil {
			t.Fatalf("expected ListReply to be non-nil")
		}
		if ev.Interactive.ListReply.ID != "item_123" || ev.Interactive.ListReply.Title != "Premium Support Plan" || ev.Interactive.ListReply.Description != "24/7 dedicated telephone and chat support" {
			t.Errorf("unexpected ListReply: %+v", ev.Interactive.ListReply)
		}
	})

	t.Run("nfm_reply plain parsing", func(t *testing.T) {
		responseJSON := `{"flow_token":"token_abc_123","screen":"SURVEY_COMPLETE","data":{"satisfaction":"5","feedback":"Great service!"}}`
		payloadObj := map[string]interface{}{
			"object": "whatsapp_business_account",
			"entry": []map[string]interface{}{
				{
					"id": "12345",
					"changes": []map[string]interface{}{
						{
							"field": "messages",
							"value": map[string]interface{}{
								"messaging_product": "whatsapp",
								"metadata": map[string]string{
									"display_phone_number": "5511999990000",
									"phone_number_id":      "phone_123",
								},
								"messages": []map[string]interface{}{
									{
										"from":      "5511988887777",
										"id":        "wamid.nfm_plain_001",
										"timestamp": "1700000000",
										"type":      "interactive",
										"interactive": map[string]interface{}{
											"type": "nfm_reply",
											"nfm_reply": map[string]string{
												"response_json": responseJSON,
												"body":          "Sent",
												"name":          "customer_survey",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		payload, _ := json.Marshal(payloadObj)

		events, err := adapter.Parse(ctx, payload, nil, conn)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		ev := events[0]
		expectedBody := "📄 *Form Submitted*\nScreen: SURVEY_COMPLETE\n- feedback: Great service!\n- satisfaction: 5"
		if ev.Body != expectedBody {
			t.Errorf("expected Body %q, got %q", expectedBody, ev.Body)
		}
		if ev.Interactive == nil || ev.Interactive.Type != "nfm_reply" {
			t.Fatalf("expected interactive nfm_reply, got %+v", ev.Interactive)
		}
		if ev.Interactive.NFMReply == nil {
			t.Fatalf("expected NFMReply to be non-nil")
		}
		if ev.Interactive.NFMReply.FlowToken != "token_abc_123" || ev.Interactive.NFMReply.Screen != "SURVEY_COMPLETE" {
			t.Errorf("unexpected NFMReply: %+v", ev.Interactive.NFMReply)
		}
		if ev.Interactive.NFMReply.Data["satisfaction"] != "5" || ev.Interactive.NFMReply.Data["feedback"] != "Great service!" {
			t.Errorf("unexpected NFMReply.Data: %+v", ev.Interactive.NFMReply.Data)
		}
	})

	t.Run("nfm_reply encrypted parsing with RSA/AES-128-GCM", func(t *testing.T) {
		flowPlaintext := `{"screen":"APPOINTMENT_BOOKED","data":{"date":"2026-09-01","doctor":"Dr. Silva"}}`
		aesKey := make([]byte, 16)
		iv := make([]byte, 12)
		rand.Read(aesKey)
		rand.Read(iv)

		encFlowData, tag, err := crypto.EncryptAES128GCM(aesKey, iv, []byte(flowPlaintext))
		if err != nil {
			t.Fatalf("EncryptAES128GCM failed: %v", err)
		}
		fullEncFlowData := append(encFlowData, tag...)

		encAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &rsaKey.PublicKey, aesKey, nil)
		if err != nil {
			t.Fatalf("EncryptRSA failed: %v", err)
		}

		responseMap := map[string]interface{}{
			"flow_token":          "enc_token_777",
			"encrypted_flow_data": base64.StdEncoding.EncodeToString(fullEncFlowData),
			"encrypted_aes_key":   base64.StdEncoding.EncodeToString(encAESKey),
			"initial_vector":      base64.StdEncoding.EncodeToString(iv),
		}
		responseJSONBytes, _ := json.Marshal(responseMap)

		payloadObj := map[string]interface{}{
			"object": "whatsapp_business_account",
			"entry": []map[string]interface{}{
				{
					"id": "12345",
					"changes": []map[string]interface{}{
						{
							"field": "messages",
							"value": map[string]interface{}{
								"messaging_product": "whatsapp",
								"metadata": map[string]string{
									"display_phone_number": "5511999990000",
									"phone_number_id":      "phone_123",
								},
								"messages": []map[string]interface{}{
									{
										"from":      "5511988887777",
										"id":        "wamid.nfm_enc_001",
										"timestamp": "1700000000",
										"type":      "interactive",
										"interactive": map[string]interface{}{
											"type": "nfm_reply",
											"nfm_reply": map[string]string{
												"response_json": string(responseJSONBytes),
												"body":          "Sent",
												"name":          "doctor_appointment",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		payload, _ := json.Marshal(payloadObj)

		events, err := adapter.Parse(ctx, payload, nil, conn)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		ev := events[0]
		expectedBody := "📄 *Form Submitted*\nScreen: APPOINTMENT_BOOKED\n- date: 2026-09-01\n- doctor: Dr. Silva"
		if ev.Body != expectedBody {
			t.Errorf("expected Body %q, got %q", expectedBody, ev.Body)
		}
		if ev.Interactive == nil || ev.Interactive.Type != "nfm_reply" {
			t.Fatalf("expected interactive nfm_reply, got %+v", ev.Interactive)
		}
		if ev.Interactive.NFMReply == nil {
			t.Fatalf("expected NFMReply to be non-nil")
		}
		if ev.Interactive.NFMReply.FlowToken != "enc_token_777" || ev.Interactive.NFMReply.Screen != "APPOINTMENT_BOOKED" {
			t.Errorf("unexpected NFMReply: %+v", ev.Interactive.NFMReply)
		}
		if ev.Interactive.NFMReply.Data["doctor"] != "Dr. Silva" || ev.Interactive.NFMReply.Data["date"] != "2026-09-01" {
			t.Errorf("unexpected NFMReply.Data: %+v", ev.Interactive.NFMReply.Data)
		}
	})

	t.Run("order message populates InboundInteractive", func(t *testing.T) {
		payload := []byte(`{
			"object": "whatsapp_business_account",
			"entry": [{
				"id": "12345",
				"changes": [{
					"field": "messages",
					"value": {
						"messaging_product": "whatsapp",
						"metadata": {
							"display_phone_number": "5511999990000",
							"phone_number_id": "phone_123"
						},
						"messages": [{
							"from": "5511988887777",
							"id": "wamid.order_001",
							"timestamp": "1700000000",
							"type": "order",
							"order": {
								"catalog_id": "cat_999",
								"text": "Gift wrap please",
								"product_items": [
									{
										"product_retailer_id": "SKU-1",
										"quantity": "3",
										"item_price": "15.00",
										"currency": "BRL"
									}
								]
							}
						}]
					}
				}]
			}]
		}`)

		events, err := adapter.Parse(ctx, payload, nil, conn)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		ev := events[0]
		if ev.Interactive == nil || ev.Interactive.Type != "order" {
			t.Fatalf("expected interactive order, got %+v", ev.Interactive)
		}
		if ev.Interactive.Order == nil {
			t.Fatalf("expected Order to be non-nil")
		}
		if ev.Interactive.Order.CatalogID != "cat_999" || ev.Interactive.Order.TotalPrice != 45.00 || ev.Interactive.Order.Currency != "BRL" {
			t.Errorf("unexpected Order: %+v", ev.Interactive.Order)
		}
		if len(ev.Interactive.Order.ProductItems) != 1 || ev.Interactive.Order.ProductItems[0].ProductRetailerID != "SKU-1" {
			t.Errorf("unexpected Order.ProductItems: %+v", ev.Interactive.Order.ProductItems)
		}
	})
}
