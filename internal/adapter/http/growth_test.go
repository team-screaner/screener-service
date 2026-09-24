//go:build integration

package http_test

import (
	"testing"

	"github.com/team-screaner/screener-service/internal/adapter/postgres"
)

func TestAssessmentSnapshotAndGrowth(t *testing.T) {
	s, db := setup(t)
	if err := postgres.Seed(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	token := register(t, s, "growth@example.com")
	list := call(t, s, "GET", "/matrices", token, "", nil, 200)
	items := list["items"].([]any)
	if len(items) < 8 {
		t.Fatal("missing seed")
	}
	m := call(t, s, "GET", "/matrices/"+items[0].(map[string]any)["id"].(string), token, "", nil, 200)
	vid := m["versions"].([]any)[0].(map[string]any)["id"].(string)
	v := call(t, s, "GET", "/matrix-versions/"+vid, token, "", nil, 200)
	levels := v["levels"].([]any)
	target := levels[len(levels)-1].(map[string]any)["id"].(string)
	um := call(t, s, "POST", "/user-matrices", token, "assign", map[string]any{"matrix_version_id": vid, "current_level_id": levels[0].(map[string]any)["id"], "target_level_id": target}, 200)
	uid := um["id"].(string)
	ctx := call(t, s, "GET", "/me/growth-context?user_matrix_id="+uid, token, "", nil, 200)
	if ctx["requirements"] == nil {
		t.Fatal("context lacks requirements")
	}
	var req string
	for _, r := range v["requirements"].([]any) {
		r := r.(map[string]any)
		if r["level_id"] == target {
			req = r["id"].(string)
			break
		}
	}

	t.Run("positive score requires evidence", func(t *testing.T) {
		call(t, s, "POST", "/assessments", token, "unsupported-positive", map[string]any{"user_matrix_id": uid, "period": "2026-Q3", "type": "self", "items": []any{map[string]any{"requirement_id": req, "score": 2, "status": "assessed"}}}, 400)
	})
	t.Run("not applicable requires zero score", func(t *testing.T) {
		call(t, s, "POST", "/assessments", token, "na-positive", map[string]any{"user_matrix_id": uid, "period": "2026-Q3", "type": "self", "items": []any{map[string]any{"requirement_id": req, "score": 1, "status": "not_applicable", "na_reason": "Outside current scope"}}}, 400)
	})
	call(t, s, "POST", "/assessments", token, "unsupported-zero", map[string]any{"user_matrix_id": uid, "period": "2026-Q3", "type": "self", "items": []any{map[string]any{"requirement_id": req, "score": 0, "status": "assessed"}}}, 200)
	var skillID, skillKey string
	for _, r := range v["requirements"].([]any) {
		row := r.(map[string]any)
		if row["id"] == req {
			skillID = row["skill_id"].(string)
		}
	}
	for _, sk := range v["skills"].([]any) {
		row := sk.(map[string]any)
		if row["skill_id"] == skillID {
			skillKey = row["skill_key"].(string)
		}
	}
	facts := call(t, s, "GET", "/fact-types", token, "", nil, 200)["items"].([]any)
	evidence := call(t, s, "POST", "/evidence", token, "matched-manual", map[string]any{"title": "Reviewed implementation", "description": "Demonstrated the target requirement", "fact_type": facts[0].(map[string]any)["key"], "source": "manual", "matches": []any{map[string]any{"skill_key": skillKey, "requirement_id": req, "confidence": 1, "reason": "Direct demonstration"}}}, 200)
	if evidence["status"] != "accepted" {
		t.Fatal("manual evidence was not accepted")
	}
	assessment := call(t, s, "POST", "/assessments", token, "assess", map[string]any{"user_matrix_id": uid, "period": "2026-Q3", "type": "self", "items": []any{map[string]any{"requirement_id": req, "score": 2, "status": "assessed", "comment": "Partial", "evidence_ids": []string{evidence["id"].(string)}}}}, 200)
	call(t, s, "POST", "/overrides", token, "override", map[string]any{"user_matrix_id": uid, "requirement_id": req, "description": "Personal expectation", "reason": "My current team scope"}, 200)
	old := call(t, s, "GET", "/assessments/"+assessment["id"].(string), token, "", nil, 200)
	if old["items"].([]any)[0].(map[string]any)["requirement_snapshot"] == "Personal expectation" {
		t.Fatal("history overwritten")
	}
	gap := call(t, s, "GET", "/me/gaps?user_matrix_id="+uid, token, "", nil, 200)
	if gap["ready"] == true || gap["percent"].(float64) == 0 {
		t.Fatalf("unexpected readiness %v", gap)
	}

	t.Run("radar contains chart data", func(t *testing.T) {
		radar := call(t, s, "GET", "/me/radar?user_matrix_id="+uid, token, "", nil, 200)
		series, ok := radar["series"].(map[string]any)
		if !ok {
			t.Fatal("missing radar series object")
		}
		if series["manager"] != nil {
			t.Fatal("manager series must be unavailable in self-assessment phase")
		}
		for _, name := range []string{"current_requirements", "target_requirements"} {
			rows, ok := series[name].([]any)
			if !ok {
				t.Errorf("%s must be chart rows, got %T", name, series[name])
				continue
			}
			if len(rows) != len(v["groups"].([]any)) {
				t.Fatalf("%s omitted competency groups", name)
			}
			for _, entry := range rows {
				row := entry.(map[string]any)
				if row["name"] == "" || row["name"] == nil {
					t.Error("radar requirement group has no name")
				}
				count, ok := row["requirements_count"].(float64)
				if !ok || count < 0 {
					t.Error("invalid requirement count")
				}
				expected := float64(0)
				if count > 0 {
					expected = 4
				}
				if row["expected_score"] != expected {
					t.Errorf("expected score=%v want%v", row["expected_score"], expected)
				}
			}
		}
		for _, entry := range series["self"].([]any) {
			row := entry.(map[string]any)
			if row["name"] == "" || row["name"] == nil {
				t.Error("self group has no name")
			}
			found := false
			for _, g := range gap["groups"].([]any) {
				group := g.(map[string]any)
				if row["group_id"] == group["group_id"] {
					found = true
					if row["percent"] != group["percent"] {
						t.Error("self percentage does not match target readiness")
					}
				}
			}
			if !found {
				t.Error("unexpected self group")
			}
		}
	})
	call(t, s, "POST", "/growth-plans", token, "plan", map[string]any{"user_matrix_id": uid, "deadline": "2026-12-31", "items": []any{map[string]any{"requirement_id": req, "action": "Deliver documented service design", "status": "planned"}}}, 200)
}
