package domain

import (
	"testing"
)

func TestSniffDelimiter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected rune
	}{
		{"comma", "nome,cidade,idade\n", ','},
		{"semicolon", "nome;cidade;idade\n", ';'},
		{"tab", "nome\tcidade\tidade\n", '\t'},
		{"comma fallback", "nome-cidade-idade\n", ','},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SniffDelimiter(tt.input); got != tt.expected {
				t.Errorf("SniffDelimiter() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSanitizePhone(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantClean  string
		wantValid  bool
	}{
		{"valid standard", "+55 (11) 99999-8888", "5511999998888", true},
		{"valid raw", "5511999998888", "5511999998888", true},
		{"short invalid", "99999-8888", "999998888", false},
		{"long invalid", "5511999998888777", "5511999998888777", false},
		{"alphabetic noise", "551199abc998888", "551199998888", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClean, gotValid := SanitizePhone(tt.input)
			if gotClean != tt.wantClean || gotValid != tt.wantValid {
				t.Errorf("SanitizePhone() = (%q, %t), want (%q, %t)", gotClean, gotValid, tt.wantClean, tt.wantValid)
			}
		})
	}
}

func TestResolveVariables(t *testing.T) {
	row := map[string]string{
		"nome":   "João",
		"cidade": "São Paulo",
		"0":      "Primeiro",
		"1":      "Segundo",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"standard", "Olá {{nome}} de {{cidade}}!", "Olá João de São Paulo!"},
		{"case insensitive", "Olá {{Nome}} de {{Cidade}}!", "Olá João de São Paulo!"},
		{"whitespace", "Olá {{  nome  }}!", "Olá João!"},
		{"missing column", "Olá {{nome}} de {{pais}}!", "Olá João de {{pais}}!"},
		{"index based", "Item {{0}} e {{1}}", "Item Primeiro e Segundo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveVariables(tt.input, row); got != tt.expected {
				t.Errorf("ResolveVariables() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCalculateDuration(t *testing.T) {
	tests := []struct {
		name         string
		totalValid   int
		batchSize    int
		delaySeconds int
		expected     int
	}{
		{"exact division", 100, 50, 5, 10},
		{"with remainder", 101, 50, 5, 15},
		{"zero valid", 0, 50, 5, 0},
		{"zero batch", 100, 0, 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateDuration(tt.totalValid, tt.batchSize, tt.delaySeconds); got != tt.expected {
				t.Errorf("CalculateDuration() = %d, want %d", got, tt.expected)
			}
		})
	}
}
