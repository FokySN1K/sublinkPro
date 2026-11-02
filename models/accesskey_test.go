package models

import (
	"strings"
	"testing"
)

func TestValidateAccessKeyDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
		wantErr     bool
	}{
		{"empty description is allowed by the model", "", false},
		{"ascii at the limit", strings.Repeat("a", AccessKeyDescriptionMaxLength), false},
		{"ascii over the limit", strings.Repeat("a", AccessKeyDescriptionMaxLength+1), true},
		{"multibyte counted by runes", strings.Repeat("键", AccessKeyDescriptionMaxLength), false},
		{"multibyte over the limit", strings.Repeat("键", AccessKeyDescriptionMaxLength+1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAccessKeyDescription(tt.description)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateAccessKeyDescription with %d runes returned %v, wantErr %v", len([]rune(tt.description)), err, tt.wantErr)
			}
		})
	}
}
