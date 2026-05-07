// Package session manages the SQLite-backed session lifecycle.
// It creates, loads, archives, and prunes user sessions, enforcing
// TTL-based expiration and tracking per-session metadata such as
// total token consumption and last activity.
package session
