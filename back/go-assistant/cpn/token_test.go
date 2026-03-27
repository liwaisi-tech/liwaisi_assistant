package cpn

import "testing"

func TestToken_IsHumanOrigin(t *testing.T) {
	tests := []struct {
		name     string
		token    *Token
		expected bool
	}{
		{"color human", &Token{Color: ColorHuman, OriginKind: NodeKindTool}, true},
		{"origin hitl", &Token{Color: ColorString, OriginKind: NodeKindHITL}, true},
		{"both human and hitl", &Token{Color: ColorHuman, OriginKind: NodeKindHITL}, true},
		{"neither", &Token{Color: ColorString, OriginKind: NodeKindLLM}, false},
		{"zero value", &Token{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.IsHumanOrigin(); got != tt.expected {
				t.Errorf("IsHumanOrigin() = %v, want %v", got, tt.expected)
			}
		})
	}
}
