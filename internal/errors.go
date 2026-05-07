// Package internal defines shared infrastructure for the Hilt application.
package internal

import "errors"

// Sentinel errors used throughout Hilt.
var (
	ErrNotFound       = errors.New("not found")
	ErrInvalidConfig  = errors.New("invalid configuration")
	ErrSessionExpired = errors.New("session expired")
)
