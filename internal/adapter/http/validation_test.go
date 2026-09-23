//go:build integration

package http_test

import "testing"

func TestContractRejectsUnknownFields(t *testing.T) {
	s, _ := setup(t)
	token := register(t, s, "validation@example.com")
	call(t, s, "POST", "/tokens", token, "unknown", map[string]any{"name": "Bad", "scopes": []string{"matrix:read"}, "expires_in_days": 1, "admin": true}, 400)
}
