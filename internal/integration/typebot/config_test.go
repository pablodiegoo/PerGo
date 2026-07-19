package typebot_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/pablojhp.pergo/internal/integration/typebot"
)

func TestConfigRoundTrip(t *testing.T) {
	t.Run("FullyPopulatedConfig", func(t *testing.T) {
		cfg := typebot.Config{
			APIURL: "https://example.com",
			Bots: []typebot.BotConfig{
				{
					BotID:          "bot-1",
					PublicToken:    "token-1",
					ConnectionID:   "conn-1",
					TriggerWords:   []string{"sales", "help"},
					IsDefault:      true,
					SessionTimeout: 30,
				},
			},
		}

		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}

		var decoded typebot.Config
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal config: %v", err)
		}

		if !reflect.DeepEqual(cfg, decoded) {
			t.Errorf("config mismatch\nexpected: %+v\ngot:      %+v", cfg, decoded)
		}
	})

	t.Run("EmptyBots", func(t *testing.T) {
		cfg := typebot.Config{
			APIURL: "https://example.com",
			Bots:   nil,
		}

		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}

		var decoded typebot.Config
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal config: %v", err)
		}

		if decoded.Bots != nil && len(decoded.Bots) != 0 {
			t.Errorf("expected empty bots list, got %v", decoded.Bots)
		}
	})

	t.Run("EmptyTriggerWords", func(t *testing.T) {
		cfg := typebot.Config{
			APIURL: "https://example.com",
			Bots: []typebot.BotConfig{
				{
					BotID:          "bot-1",
					PublicToken:    "token-1",
					ConnectionID:   "conn-1",
					TriggerWords:   nil,
					IsDefault:      false,
					SessionTimeout: 0,
				},
			},
		}

		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}

		var decoded typebot.Config
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal config: %v", err)
		}

		if decoded.Bots[0].TriggerWords != nil && len(decoded.Bots[0].TriggerWords) != 0 {
			t.Errorf("expected empty trigger words list, got %v", decoded.Bots[0].TriggerWords)
		}
	})

	t.Run("ZeroSessionTimeout", func(t *testing.T) {
		cfg := typebot.Config{
			APIURL: "https://example.com",
			Bots: []typebot.BotConfig{
				{
					BotID:          "bot-1",
					PublicToken:    "token-1",
					ConnectionID:   "conn-1",
					TriggerWords:   []string{"sales"},
					IsDefault:      true,
					SessionTimeout: 0,
				},
			},
		}

		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}

		var decoded typebot.Config
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal config: %v", err)
		}

		if decoded.Bots[0].SessionTimeout != 0 {
			t.Errorf("expected zero session timeout, got %d", decoded.Bots[0].SessionTimeout)
		}
	})

	t.Run("MultipleBots", func(t *testing.T) {
		cfg := typebot.Config{
			APIURL: "https://example.com",
			Bots: []typebot.BotConfig{
				{
					BotID:          "bot-1",
					PublicToken:    "token-1",
					ConnectionID:   "conn-1",
					TriggerWords:   []string{"sales"},
					IsDefault:      false,
					SessionTimeout: 30,
				},
				{
					BotID:          "bot-2",
					PublicToken:    "token-2",
					ConnectionID:   "conn-2",
					TriggerWords:   []string{"support"},
					IsDefault:      true,
					SessionTimeout: 15,
				},
			},
		}

		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}

		var decoded typebot.Config
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal config: %v", err)
		}

		if !reflect.DeepEqual(cfg, decoded) {
			t.Errorf("config mismatch\nexpected: %+v\ngot:      %+v", cfg, decoded)
		}
	})
}
