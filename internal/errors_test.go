package internal

import (
	"errors"
	"testing"
)

func TestSentinelsExist(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"ErrNotFound", ErrNotFound},
		{"ErrInvalidConfig", ErrInvalidConfig},
		{"ErrSessionExpired", ErrSessionExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatalf("%s is nil", tt.name)
			}
			if !errors.Is(tt.err, tt.err) {
				t.Fatalf("%s does not match itself with errors.Is", tt.name)
			}
		})
	}
}
