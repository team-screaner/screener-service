//go:build integration

package http_test

import "testing"

func TestContractRejectsUnknownFields(t *testing.T) {
	s, _ := setup(t)
	token := register(t, s, "validation@example.com")
	call(t, s, "POST", "/tokens", token, "unknown", map[string]any{"name": "Bad", "scopes": []string{"matrix:read"}, "expires_in_days": 1, "admin": true}, 400)
}

func TestTokenListUsesBoundedCursorPagination(t *testing.T) {
	s, _ := setup(t)
	token := register(t, s, "token-pages@example.com")
	for _, key := range []string{"one", "two"} {
		call(t, s, "POST", "/tokens", token, key, map[string]any{"name": key, "scopes": []string{"matrix:read"}}, 200)
	}
	first := call(t, s, "GET", "/tokens?limit=1", token, "", nil, 200)
	if len(first["items"].([]any)) != 1 || first["next_cursor"] == "" {
		t.Fatalf("unbounded token page: %v", first)
	}
	second := call(t, s, "GET", "/tokens?limit=1&cursor="+first["next_cursor"].(string), token, "", nil, 200)
	if len(second["items"].([]any)) != 1 || second["items"].([]any)[0].(map[string]any)["id"] == first["items"].([]any)[0].(map[string]any)["id"] {
		t.Fatal("cursor repeated token")
	}
}
