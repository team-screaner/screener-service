//go:build integration

package http_test

import "testing"

func TestAuditIdentifiesCreatedResource(t *testing.T) {
	s, _ := setup(t)
	token := register(t, s, "audit@example.com")
	org := call(t, s, "POST", "/organizations", token, "audit-org", map[string]any{"name": "Audited"}, 200)
	audit := call(t, s, "GET", "/audit", token, "", nil, 200)
	for _, item := range audit["items"].([]any) {
		entry := item.(map[string]any)
		if entry["operation"] == "createOrganization" {
			if entry["resource_id"] != org["id"] {
				t.Fatalf("audit resource=%v, want %v", entry["resource_id"], org["id"])
			}
			return
		}
	}
	t.Fatal("missing create audit")
}
