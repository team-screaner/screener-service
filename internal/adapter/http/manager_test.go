//go:build integration

package http_test

import (
	"bytes"
	"testing"

	"github.com/team-screaner/screener-service/internal/adapter/postgres"
	"github.com/xuri/excelize/v2"
)

func TestManagerReviewAndRadar(t *testing.T) {
	s, db := setup(t)
	if err := postgres.Seed(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	owner := register(t, s, "review-owner@example.com")
	manager := register(t, s, "manager@example.com")
	stranger := register(t, s, "stranger@example.com")
	managerID := call(t, s, "GET", "/me", manager, "", nil, 200)["id"]
	matrices := call(t, s, "GET", "/matrices", owner, "", nil, 200)["items"].([]any)
	m := call(t, s, "GET", "/matrices/"+matrices[0].(map[string]any)["id"].(string), owner, "", nil, 200)
	vid := m["versions"].([]any)[0].(map[string]any)["id"].(string)
	v := call(t, s, "GET", "/matrix-versions/"+vid, owner, "", nil, 200)
	levels := v["levels"].([]any)
	target := levels[len(levels)-1].(map[string]any)["id"]
	um := call(t, s, "POST", "/user-matrices", owner, "assign", map[string]any{"matrix_version_id": vid, "current_level_id": levels[0].(map[string]any)["id"], "target_level_id": target}, 200)["id"].(string)
	reviewerPath := "/user-matrices/" + um + "/reviewer"
	reviewPath := "/reviews/" + um
	call(t, s, "GET", reviewPath, stranger, "", nil, 404)
	call(t, s, "PUT", reviewerPath, stranger, "forged-grant", map[string]any{"email": "manager@example.com"}, 404)
	call(t, s, "PUT", reviewerPath, owner, "self-grant", map[string]any{"email": "review-owner@example.com"}, 400)
	call(t, s, "PUT", reviewerPath, owner, "missing-grant", map[string]any{"email": "nobody@example.com"}, 400)
	grant := call(t, s, "PUT", reviewerPath, owner, "grant", map[string]any{"email": "manager@example.com"}, 200)
	if grant["reviewer"].(map[string]any)["id"] != managerID {
		t.Fatal("wrong reviewer")
	}
	call(t, s, "GET", reviewPath, owner, "", nil, 404)
	assignments := call(t, s, "GET", "/reviews", manager, "", nil, 200)["items"].([]any)
	if len(assignments) != 1 || assignments[0].(map[string]any)["id"] != um {
		t.Fatal("missing review assignment")
	}
	router := contractRouter(t)
	contractValidate(t, router, contractMustRequest(t, s, "GET", reviewerPath, owner, "", nil, 200))
	contractValidate(t, router, contractMustRequest(t, s, "GET", reviewPath, manager, "", nil, 200))
	contractValidate(t, router, contractMustRequest(t, s, "GET", "/reviews", manager, "", nil, 200))
	contractValidate(t, router, contractMustRequest(t, s, "GET", "/me/radar?user_matrix_id="+um, owner, "", nil, 200))
	empty := call(t, s, "GET", reviewPath, manager, "", nil, 200)
	if empty["assessment"] != nil {
		t.Fatal("invented self assessment")
	}
	factType := call(t, s, "GET", "/fact-types", owner, "", nil, 200)["items"].([]any)[0].(map[string]any)["key"]
	skills := map[string]string{}
	for _, raw := range v["skills"].([]any) {
		sk := raw.(map[string]any)
		skills[sk["skill_id"].(string)] = sk["skill_key"].(string)
	}
	selfItems := []any{}
	managerItems := []any{}
	for _, raw := range v["requirements"].([]any) {
		req := raw.(map[string]any)
		if req["level_id"] != target {
			continue
		}
		rid := req["id"].(string)
		ev := call(t, s, "POST", "/evidence", owner, "fact-"+rid, map[string]any{"title": "Completed work", "description": "Demonstrated requirement", "fact_type": factType, "source": "manual", "matches": []any{map[string]any{"skill_key": skills[req["skill_id"].(string)], "requirement_id": rid, "confidence": 1, "reason": "Result"}}}, 200)
		selfItems = append(selfItems, map[string]any{"requirement_id": rid, "score": 4, "status": "assessed", "evidence_ids": []any{ev["id"]}})
		managerItems = append(managerItems, map[string]any{"requirement_id": rid, "score": 2, "status": "assessed", "comment": "Needs independence", "evidence_ids": []any{ev["id"]}})
	}
	selfBody := map[string]any{"user_matrix_id": um, "period": "2026-Q3", "type": "self", "items": selfItems}
	self := call(t, s, "POST", "/assessments", owner, "self", selfBody, 200)
	body := map[string]any{"user_matrix_id": um, "period": "2026-Q3", "type": "manager", "reviewed_assessment_id": self["id"], "items": managerItems}
	call(t, s, "POST", "/assessments", stranger, "foreign-review", body, 404)
	call(t, s, "POST", "/assessments", owner, "self-review", body, 404)
	body["period"] = "2026-Q2"
	call(t, s, "POST", "/assessments", manager, "wrong-period", body, 400)
	body["period"] = "2026-Q3"
	body["items"] = managerItems[:1]
	call(t, s, "POST", "/assessments", manager, "incomplete", body, 400)
	body["items"] = managerItems
	// A manager cannot attach their own evidence or rate without employee evidence.
	first := managerItems[0].(map[string]any)
	employeeEvidence := first["evidence_ids"]
	first["evidence_ids"] = []any{}
	call(t, s, "POST", "/assessments", manager, "unsupported-review", body, 400)
	first["evidence_ids"] = []any{postgres.ID()}
	call(t, s, "POST", "/assessments", manager, "foreign-evidence", body, 400)
	first["evidence_ids"] = employeeEvidence
	review := call(t, s, "POST", "/assessments", manager, "review", body, 200)
	if review["assessor_id"] != managerID || review["reviewed_assessment_id"] != self["id"] || review["user_id"] != self["user_id"] {
		t.Fatal("incorrect attribution")
	}
	t.Run("personal export retains self assessment", func(t *testing.T) {
		response := contractMustRequest(t, s, "GET", "/export/xlsx?user_matrix_id="+um, owner, "", nil, 200)
		book, openErr := excelize.OpenReader(bytes.NewReader(response.body))
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer func() {
			if closeErr := book.Close(); closeErr != nil {
				t.Error(closeErr)
			}
		}()
		rows, readErr := book.GetRows("Assessment")
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(rows) < 2 {
			t.Fatal("missing assessment export")
		}
		for _, row := range rows[1:] {
			if len(row) < 7 || row[0] != self["id"] || row[2] != "self" || row[6] != "4" {
				t.Fatalf("manager assessment replaced self export: %v", row)
			}
		}
	})
	replay := call(t, s, "POST", "/assessments", manager, "review", body, 200)
	if replay["id"] != review["id"] {
		t.Fatal("retry duplicated assessment")
	}
	call(t, s, "GET", "/assessments/"+review["id"].(string), stranger, "", nil, 404)
	stored := call(t, s, "GET", "/assessments/"+review["id"].(string), owner, "", nil, 200)
	if stored["type"] != "manager" {
		t.Fatal("manager snapshot missing")
	}
	radar := call(t, s, "GET", "/me/radar?user_matrix_id="+um, owner, "", nil, 200)
	series := radar["series"].(map[string]any)
	if radar["percent"] != float64(100) || radar["manager_assessment_id"] != review["id"] || radar["period"] != "2026-Q3" {
		t.Fatalf("bad comparison: %v", radar)
	}
	selfGroups := series["self"].([]any)
	managerGroups := series["manager"].([]any)
	if len(selfGroups) != len(managerGroups) {
		t.Fatal("mismatched axes")
	}
	for i, raw := range selfGroups {
		a := raw.(map[string]any)
		b := managerGroups[i].(map[string]any)
		if a["group_id"] != b["group_id"] || a["percent"] != float64(100) || b["percent"] != float64(50) {
			t.Fatalf("wrong group comparison %v %v", a, b)
		}
	}
	contractValidate(t, router, contractMustRequest(t, s, "GET", "/me/radar?user_matrix_id="+um, owner, "", nil, 200))
	contractValidate(t, router, contractMustRequest(t, s, "GET", reviewPath, manager, "", nil, 200))
	for _, key := range skills {
		call(t, s, "POST", "/evidence", owner, "unshared", map[string]any{"title": "Private unlinked evidence", "description": "Not shared with reviewer", "fact_type": factType, "source": "manual", "matches": []any{map[string]any{"skill_key": key, "confidence": 1, "reason": "Skill only"}}}, 200)
		break
	}
	ctx := call(t, s, "GET", reviewPath, manager, "", nil, 200)
	for _, raw := range ctx["context"].(map[string]any)["evidence"].([]any) {
		if raw.(map[string]any)["title"] == "Private unlinked evidence" {
			t.Fatal("unshared evidence leaked")
		}
	}
	if ctx["manager_assessment"].(map[string]any)["id"] != review["id"] {
		t.Fatal("review not available to manager")
	}
	agent := call(t, s, "POST", "/tokens", manager, "agent", map[string]any{"name": "reader", "scopes": []string{"assessment:read", "matrix:read", "evidence:read"}}, 200)["token"].(string)
	call(t, s, "GET", reviewPath, agent, "", nil, 403)
	call(t, s, "POST", "/assessments", agent, "agent-review", body, 403)
	// Revocation must also block a cached idempotency response.
	call(t, s, "DELETE", reviewerPath, owner, "revoke", nil, 200)
	contractValidate(t, router, contractMustRequest(t, s, "GET", reviewerPath, owner, "", nil, 200))
	call(t, s, "GET", reviewPath, manager, "", nil, 404)
	call(t, s, "POST", "/assessments", manager, "review", body, 404)
	if len(call(t, s, "GET", "/reviews", manager, "", nil, 200)["items"].([]any)) != 0 {
		t.Fatal("revoked assignment visible")
	}
	// Published history remains, but new self assessment must not pair with old manager data.
	call(t, s, "POST", "/assessments", owner, "self-new", selfBody, 200)
	radar = call(t, s, "GET", "/me/radar?user_matrix_id="+um, owner, "", nil, 200)
	if radar["series"].(map[string]any)["manager"] != nil {
		t.Fatal("paired unrelated assessment snapshots")
	}
	call(t, s, "PUT", reviewerPath, owner, "regrant", map[string]any{"email": "manager@example.com"}, 200)
	call(t, s, "POST", "/assessments", manager, "stale-review", body, 409)
}
