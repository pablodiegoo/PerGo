package domain

import (
	"encoding/json"
	"testing"
)

func TestMessageStatusValues(t *testing.T) {
	tests := []struct {
		name     string
		status   MessageStatus
		expected string
	}{
		{"queued", StatusQueued, "queued"},
		{"sent", StatusSent, "sent"},
		{"delivered", StatusDelivered, "delivered"},
		{"read", StatusRead, "read"},
		{"failed", StatusFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("status %s = %q, want %q", tt.name, string(tt.status), tt.expected)
			}
		})
	}
}

func TestValidateMessageValid(t *testing.T) {
	req := &CreateMessageRequest{
		To:      "+1234567890",
		Channel: "whatsapp",
		Body:    "Hello",
	}
	if err := ValidateMessage(req); err != nil {
		t.Errorf("expected nil error, got %+v", err)
	}
}

func TestValidateMessageMissingTo(t *testing.T) {
	req := &CreateMessageRequest{
		Channel: "whatsapp",
		Body:    "Hello",
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for missing to, got nil")
	}
	if err.Code != "invalid_payload" {
		t.Errorf("code = %q, want %q", err.Code, "invalid_payload")
	}
	if len(err.Details) != 1 || err.Details[0].Field != "to" {
		t.Errorf("expected field error for 'to', got %+v", err.Details)
	}
}

func TestValidateMessageMissingChannel(t *testing.T) {
	req := &CreateMessageRequest{
		To:   "+1234567890",
		Body: "Hello",
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for missing channel, got nil")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "channel" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'channel', got %+v", err.Details)
	}
}

func TestValidateMessageZeroTTL(t *testing.T) {
	zero := 0
	req := &CreateMessageRequest{
		To:         "+1234567890",
		Channel:    "whatsapp",
		TTLSeconds: &zero,
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for zero TTL, got nil")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "ttl_seconds" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'ttl_seconds', got %+v", err.Details)
	}
}

func TestValidateMessageNegativeTTL(t *testing.T) {
	neg := -5
	req := &CreateMessageRequest{
		To:         "+1234567890",
		Channel:    "whatsapp",
		TTLSeconds: &neg,
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for negative TTL, got nil")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "ttl_seconds" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'ttl_seconds', got %+v", err.Details)
	}
}

func TestValidateMessageNilTTL(t *testing.T) {
	req := &CreateMessageRequest{
		To:         "+1234567890",
		Channel:    "whatsapp",
		Body:       "Hello",
		TTLSeconds: nil,
	}
	if err := ValidateMessage(req); err != nil {
		t.Errorf("expected nil error for nil TTL, got %+v", err)
	}
}

func TestValidateMessageEmptyBodyAndMedia(t *testing.T) {
	req := &CreateMessageRequest{
		To:      "+1234567890",
		Channel: "whatsapp",
		Body:    "",
		Media:   nil,
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for empty body and media, got nil")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "body" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'body', got %+v", err.Details)
	}
}

func TestValidateMessageMedia(t *testing.T) {
	tests := []struct {
		name        string
		media       *Media
		body        string
		expectError bool
		errField    string
	}{
		{
			name: "valid image",
			media: &Media{
				MediaURL:  "https://example.com/image.png",
				MediaType: "image",
			},
			expectError: false,
		},
		{
			name: "valid audio",
			media: &Media{
				MediaURL:  "https://example.com/audio.mp3",
				MediaType: "audio",
			},
			expectError: false,
		},
		{
			name: "valid voice note",
			media: &Media{
				MediaURL:  "https://example.com/voice.ogg",
				MediaType: "voice",
				PTT:       true,
			},
			expectError: false,
		},
		{
			name: "valid document with filename",
			media: &Media{
				MediaURL:  "https://example.com/doc.pdf",
				MediaType: "document",
				Filename:  "doc.pdf",
			},
			expectError: false,
		},
		{
			name: "document missing filename",
			media: &Media{
				MediaURL:  "https://example.com/doc.pdf",
				MediaType: "document",
			},
			expectError: true,
			errField:    "media.filename",
		},
		{
			name: "invalid media type",
			media: &Media{
				MediaURL:  "https://example.com/file.txt",
				MediaType: "text",
			},
			expectError: true,
			errField:    "media.media_type",
		},
		{
			name: "empty media url",
			media: &Media{
				MediaURL:  "",
				MediaType: "image",
			},
			expectError: true,
			errField:    "media.media_url",
		},
		{
			name: "invalid url scheme",
			media: &Media{
				MediaURL:  "ftp://example.com/image.png",
				MediaType: "image",
			},
			expectError: true,
			errField:    "media.media_url",
		},
		{
			name: "both body and media present",
			body: "Hello",
			media: &Media{
				MediaURL:  "https://example.com/image.png",
				MediaType: "image",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &CreateMessageRequest{
				To:      "+1234567890",
				Channel: "whatsapp",
				Body:    tt.body,
				Media:   tt.media,
			}
			err := ValidateMessage(req)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				found := false
				for _, d := range err.Details {
					if d.Field == tt.errField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected field error for %q, got %+v", tt.errField, err.Details)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %+v", err)
				}
			}
		})
	}
}

func TestErrorResponseJSON(t *testing.T) {
	resp := ErrorResponse{
		Code:     "invalid_payload",
		Message:  "request body validation failed",
		MoreInfo: "https://docs.pergo.dev/errors/invalid_payload",
		Details: []FieldError{
			{Field: "to", Message: "is required"},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal ErrorResponse: %v", err)
	}

	var decoded ErrorResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ErrorResponse: %v", err)
	}

	if decoded.Code != resp.Code {
		t.Errorf("code = %q, want %q", decoded.Code, resp.Code)
	}
	if decoded.Message != resp.Message {
		t.Errorf("message = %q, want %q", decoded.Message, resp.Message)
	}
	if decoded.MoreInfo != resp.MoreInfo {
		t.Errorf("more_info = %q, want %q", decoded.MoreInfo, resp.MoreInfo)
	}
	if len(decoded.Details) != 1 || decoded.Details[0].Field != "to" {
		t.Errorf("details = %+v, want [{Field:to Message:is required}]", decoded.Details)
	}
}

func TestValidateMessageTemplateValid(t *testing.T) {
	req := &CreateMessageRequest{
		To:           "+1234567890",
		Channel:      "whatsapp_cloud",
		TemplateName: "welcome_template",
		Language:     "en",
	}
	if err := ValidateMessage(req); err != nil {
		t.Errorf("expected nil error for valid template request, got %+v", err)
	}
}

func TestValidateMessageTemplateMissingLanguage(t *testing.T) {
	req := &CreateMessageRequest{
		To:           "+1234567890",
		Channel:      "vendas-waba",
		TemplateName: "welcome_template",
		Language:     "",
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for template missing language, got nil")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "language" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'language', got %+v", err.Details)
	}
}

func TestValidateMessageFallbackBehavior(t *testing.T) {
	tests := []struct {
		name        string
		behavior    string
		expectError bool
	}{
		{"empty is valid", "", false},
		{"degrade is valid", "degrade", false},
		{"fail is valid", "fail", false},
		{"invalid behavior", "ignore", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &CreateMessageRequest{
				To:               "+1234567890",
				Channel:          "whatsapp",
				Body:             "Hello",
				FallbackBehavior: tt.behavior,
			}
			err := ValidateMessage(req)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				found := false
				for _, d := range err.Details {
					if d.Field == "fallback_behavior" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected field error for 'fallback_behavior', got %+v", err.Details)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %+v", err)
				}
			}
		})
	}
}

func TestValidateMessageInteractiveStructure(t *testing.T) {
	tests := []struct {
		name        string
		interactive *Interactive
		expectError bool
		errField    string
	}{
		{
			name: "valid button",
			interactive: &Interactive{
				Type: "button",
				Body: TextContent{Text: "Choose an option"},
				Action: Action{
					Buttons: []Button{
						{Type: "reply", Reply: Reply{ID: "1", Title: "Yes"}},
					},
				},
			},
			expectError: false,
		},
		{
			name: "missing type",
			interactive: &Interactive{
				Body: TextContent{Text: "Choose an option"},
			},
			expectError: true,
			errField:    "interactive.type",
		},
		{
			name: "missing body text",
			interactive: &Interactive{
				Type: "button",
				Action: Action{
					Buttons: []Button{
						{Type: "reply", Reply: Reply{ID: "1", Title: "Yes"}},
					},
				},
			},
			expectError: true,
			errField:    "interactive.body.text",
		},
		{
			name: "button missing buttons array",
			interactive: &Interactive{
				Type: "button",
				Body: TextContent{Text: "Choose an option"},
				Action: Action{},
			},
			expectError: true,
			errField:    "interactive.action.buttons",
		},
		{
			name: "list missing sections",
			interactive: &Interactive{
				Type: "list",
				Body: TextContent{Text: "Choose an option"},
				Action: Action{},
			},
			expectError: true,
			errField:    "interactive.action.sections",
		},
		{
			name: "allows 4+ buttons (deferred validation)",
			interactive: &Interactive{
				Type: "button",
				Body: TextContent{Text: "Choose an option"},
				Action: Action{
					Buttons: []Button{
						{Type: "reply", Reply: Reply{ID: "1", Title: "Opt 1"}},
						{Type: "reply", Reply: Reply{ID: "2", Title: "Opt 2"}},
						{Type: "reply", Reply: Reply{ID: "3", Title: "Opt 3"}},
						{Type: "reply", Reply: Reply{ID: "4", Title: "Opt 4"}},
						{Type: "reply", Reply: Reply{ID: "5", Title: "Opt 5"}},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &CreateMessageRequest{
				To:          "+1234567890",
				Channel:     "whatsapp",
				Interactive: tt.interactive,
			}
			err := ValidateMessage(req)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				found := false
				for _, d := range err.Details {
					if d.Field == tt.errField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected field error for %q, got %+v", tt.errField, err.Details)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %+v", err)
				}
			}
		})
	}
}

func TestProductPayload_Structs(t *testing.T) {
	if MessageTypeProduct != "product" {
		t.Errorf("expected MessageTypeProduct to be 'product', got %q", MessageTypeProduct)
	}
	if MessageTypeProductList != "product_list" {
		t.Errorf("expected MessageTypeProductList to be 'product_list', got %q", MessageTypeProductList)
	}

	payload := ProductPayload{
		CatalogID:         "cat_123",
		ProductRetailerID: "sku_99",
		Header:            "Product Header",
		Body:              "Product Body",
		Footer:            "Product Footer",
		Sections: []ProductSection{
			{
				Title: "Section 1",
				ProductItems: []ProductItem{
					{
						ProductRetailerID: "sku_01",
						ItemPrice:         19.99,
						Currency:          "BRL",
						Quantity:          2,
					},
				},
			},
		},
	}

	req := CreateMessageRequest{
		To:      "+5511999999999",
		Channel: "whatsapp_cloud",
		Type:    MessageTypeProductList,
		Product: &payload,
	}

	if req.Type != "product_list" || req.Product == nil || req.Product.CatalogID != "cat_123" {
		t.Errorf("unexpected CreateMessageRequest struct layout: %+v", req)
	}

	qMsg := QueueMessage{
		Type:    MessageTypeProduct,
		Product: &payload,
	}
	if qMsg.Type != "product" || qMsg.Product == nil {
		t.Errorf("unexpected QueueMessage struct layout: %+v", qMsg)
	}
}

func TestValidateMessage_ProductPayload(t *testing.T) {
	t.Run("valid single product", func(t *testing.T) {
		req := &CreateMessageRequest{
			To:      "+5511999999999",
			Channel: "whatsapp_cloud",
			Type:    MessageTypeProduct,
			Product: &ProductPayload{
				CatalogID:         "cat_123",
				ProductRetailerID: "sku_100",
			},
		}
		if err := ValidateMessage(req); err != nil {
			t.Errorf("expected no error, got %+v", err)
		}
	})

	t.Run("single product missing product struct", func(t *testing.T) {
		req := &CreateMessageRequest{
			To:      "+5511999999999",
			Channel: "whatsapp_cloud",
			Type:    MessageTypeProduct,
		}
		err := ValidateMessage(req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Code != "invalid_product_payload" {
			t.Errorf("code = %q, want invalid_product_payload", err.Code)
		}
		found := false
		for _, d := range err.Details {
			if d.Field == "product" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected field error for product, got %+v", err.Details)
		}
	})

	t.Run("single product missing SKU", func(t *testing.T) {
		req := &CreateMessageRequest{
			To:      "+5511999999999",
			Channel: "whatsapp_cloud",
			Type:    MessageTypeProduct,
			Product: &ProductPayload{
				CatalogID: "cat_123",
			},
		}
		err := ValidateMessage(req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Code != "invalid_product_payload" {
			t.Errorf("code = %q, want invalid_product_payload", err.Code)
		}
		found := false
		for _, d := range err.Details {
			if d.Field == "product.product_retailer_id" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected field error for product.product_retailer_id, got %+v", err.Details)
		}
	})

	t.Run("valid product list", func(t *testing.T) {
		req := &CreateMessageRequest{
			To:      "+5511999999999",
			Channel: "whatsapp_cloud",
			Type:    MessageTypeProductList,
			Product: &ProductPayload{
				CatalogID: "cat_123",
				Sections: []ProductSection{
					{
						Title: "Electronics",
						ProductItems: []ProductItem{
							{ProductRetailerID: "sku_1"},
							{ProductRetailerID: "sku_2"},
						},
					},
					{
						Title: "Accessories",
						ProductItems: []ProductItem{
							{ProductRetailerID: "sku_3"},
							{ProductRetailerID: "sku_4"},
							{ProductRetailerID: "sku_5"},
						},
					},
				},
			},
		}
		if err := ValidateMessage(req); err != nil {
			t.Errorf("expected no error, got %+v", err)
		}
	})

	t.Run("product list exceeds 10 sections", func(t *testing.T) {
		sections := make([]ProductSection, 11)
		for i := 0; i < 11; i++ {
			sections[i] = ProductSection{
				Title: "Sec",
				ProductItems: []ProductItem{
					{ProductRetailerID: "sku_1"},
				},
			}
		}
		req := &CreateMessageRequest{
			To:      "+5511999999999",
			Channel: "whatsapp_cloud",
			Type:    MessageTypeProductList,
			Product: &ProductPayload{
				CatalogID: "cat_123",
				Sections:  sections,
			},
		}
		err := ValidateMessage(req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Code != "invalid_product_payload" {
			t.Errorf("code = %q, want invalid_product_payload", err.Code)
		}
		found := false
		for _, d := range err.Details {
			if d.Field == "product.sections" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected field error for product.sections, got %+v", err.Details)
		}
	})

	t.Run("product list title exceeds 24 chars", func(t *testing.T) {
		req := &CreateMessageRequest{
			To:      "+5511999999999",
			Channel: "whatsapp_cloud",
			Type:    MessageTypeProductList,
			Product: &ProductPayload{
				CatalogID: "cat_123",
				Sections: []ProductSection{
					{
						Title: "This section title is way too long for Meta API", // 46 chars
						ProductItems: []ProductItem{
							{ProductRetailerID: "sku_1"},
						},
					},
				},
			},
		}
		err := ValidateMessage(req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Code != "invalid_product_payload" {
			t.Errorf("code = %q, want invalid_product_payload", err.Code)
		}
		found := false
		for _, d := range err.Details {
			if d.Field == "product.sections[0].title" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected field error for product.sections[0].title, got %+v", err.Details)
		}
	})

	t.Run("product list total items exceed 30", func(t *testing.T) {
		items := make([]ProductItem, 31)
		for i := 0; i < 31; i++ {
			items[i] = ProductItem{ProductRetailerID: "sku_x"}
		}
		req := &CreateMessageRequest{
			To:      "+5511999999999",
			Channel: "whatsapp_cloud",
			Type:    MessageTypeProductList,
			Product: &ProductPayload{
				CatalogID: "cat_123",
				Sections: []ProductSection{
					{
						Title:        "Large Catalog",
						ProductItems: items,
					},
				},
			},
		}
		err := ValidateMessage(req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Code != "invalid_product_payload" {
			t.Errorf("code = %q, want invalid_product_payload", err.Code)
		}
		found := false
		for _, d := range err.Details {
			if d.Field == "product.sections" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected field error for product.sections, got %+v", err.Details)
		}
	})

	t.Run("product list item missing SKU", func(t *testing.T) {
		req := &CreateMessageRequest{
			To:      "+5511999999999",
			Channel: "whatsapp_cloud",
			Type:    MessageTypeProductList,
			Product: &ProductPayload{
				CatalogID: "cat_123",
				Sections: []ProductSection{
					{
						Title: "Electronics",
						ProductItems: []ProductItem{
							{ProductRetailerID: ""},
						},
					},
				},
			},
		}
		err := ValidateMessage(req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Code != "invalid_product_payload" {
			t.Errorf("code = %q, want invalid_product_payload", err.Code)
		}
		found := false
		for _, d := range err.Details {
			if d.Field == "product.sections[0].product_items[0].product_retailer_id" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected field error for product.sections[0].product_items[0].product_retailer_id, got %+v", err.Details)
		}
	})
}


