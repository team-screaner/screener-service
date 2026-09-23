//go:build integration

package http_test

import (
	"testing"
)

func securityMatrixInput(skillID string) map[string]any {
	return map[string]any{
		"name": "Unannounced internal promotion track", "description": "Confidential draft description",
		"levels":       []any{map[string]any{"key": "L1", "name": "First"}},
		"groups":       []any{map[string]any{"key": "security", "name": "Security", "weight": 1}},
		"skills":       []any{map[string]any{"skill_id": skillID, "group_key": "security", "weight": 1, "required": true, "active": true}},
		"requirements": []any{map[string]any{"skill_id": skillID, "level_key": "L1", "description": "Review authorization boundaries", "weight": 1, "required": true, "critical": true}},
	}
}

func TestPublicDraftRemainsPrivateUntilPublication(t *testing.T) {
	s, db := setup(t)
	owner := register(t, s, "draft-owner@example.test")
	other := register(t, s, "draft-other@example.test")
	skillID := "01999999-0000-7000-8000-000000000021"
	if _, err := db.ExecContext(t.Context(), `INSERT INTO skill_categories(key,name) VALUES('security','Security'); INSERT INTO skills(id,key,name,category,scope) VALUES('01999999-0000-7000-8000-000000000021','authorization','Authorization','security','system')`); err != nil {
		t.Fatal(err)
	}
	body := securityMatrixInput(skillID)
	body["visibility"] = "public"
	matrix := call(t, s, "POST", "/matrices", owner, "public-draft", body, 200)
	id := matrix["id"].(string)
	version := matrix["version"].(map[string]any)["id"].(string)
	call(t, s, "GET", "/matrix-versions/"+version, other, "", nil, 404)
	call(t, s, "GET", "/matrices/"+id, other, "", nil, 404)
	call(t, s, "POST", "/matrix-versions/"+version+"/publish", owner, "publish", nil, 200)
	call(t, s, "GET", "/matrices/"+id, other, "", nil, 200)
}

func TestIdempotencyReplayRechecksRevokedOrganizationWriteAccess(t *testing.T) {
	s, db := setup(t)
	admin := register(t, s, "former-admin@example.test")
	userID := call(t, s, "GET", "/me", admin, "", nil, 200)["id"].(string)
	organizationID := "01999999-0000-7000-8000-000000000022"
	skillID := "01999999-0000-7000-8000-000000000023"
	if _, err := db.ExecContext(t.Context(), `INSERT INTO skill_categories(key,name) VALUES('security','Security'); INSERT INTO skills(id,key,name,category,scope) VALUES('01999999-0000-7000-8000-000000000023','authorization','Authorization','security','system'); INSERT INTO organizations(id,name) VALUES('01999999-0000-7000-8000-000000000022','Test organization')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO organization_members(organization_id,user_id,role) VALUES($1,$2,'admin')`, organizationID, userID); err != nil {
		t.Fatal(err)
	}
	body := securityMatrixInput(skillID)
	body["scope"] = "organization"
	body["organization_id"] = organizationID
	matrix := call(t, s, "POST", "/matrices", admin, "org-matrix", body, 200)
	path := "/matrices/" + matrix["id"].(string) + "/versions"
	call(t, s, "POST", path, admin, "original-authorized-command", body, 200)
	// Model an organization owner revoking this account's write permission.
	if _, err := db.ExecContext(t.Context(), `UPDATE organization_members SET role='member' WHERE organization_id=$1 AND user_id=$2`, organizationID, userID); err != nil {
		t.Fatal(err)
	}
	call(t, s, "POST", path, admin, "fresh-denied-command", body, 404)
	call(t, s, "POST", path, admin, "original-authorized-command", body, 404)
}
