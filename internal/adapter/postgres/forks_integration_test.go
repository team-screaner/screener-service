//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/team-screaner/screener-service/internal/domain"
	tc "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func forkDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pg, err := tc.Run(ctx, "postgres:17-alpine", tc.WithDatabase("forks"), tc.WithUsername("test"), tc.WithPassword("test"), tc.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if cleanupErr := pg.Terminate(cleanupCtx); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := goose.UpContext(ctx, db, "../../../migrations"); err != nil {
		t.Fatal(err)
	}
	return db
}
func forkRun(t *testing.T, db *sql.DB, user, op, id string, body any) (domain.Result, error) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	c := domain.Command{Actor: domain.Actor{UserID: user, Kind: "session"}, Operation: op, ID: id, Body: b}
	var out domain.Result
	switch op {
	case "forkMatrix", "duplicateMatrix", "getUpstream", "reviewUpstream":
		out, err = handleFork(t.Context(), tx, c)
	default:
		out, err = handleMatrix(t.Context(), tx, c)
	}
	if err != nil {
		return nil, mapError(err)
	}
	return out, tx.Commit()
}
func forkMust(t *testing.T, out domain.Result, err error) domain.Result {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func forkFixture(t *testing.T, db *sql.DB) (string, string, string, MatrixInput) {
	t.Helper()
	owner, other, skill := ID(), ID(), ID()
	if _, execErr := db.ExecContext(t.Context(), `INSERT INTO users(id,email,name,password_hash) VALUES($1,'owner@example.test','Owner','x'),($2,'other@example.test','Other','x')`, owner, other); execErr != nil {
		t.Fatal(execErr)
	}
	if _, execErr := db.ExecContext(t.Context(), `INSERT INTO skill_categories(key,name) VALUES('backend','Backend')`); execErr != nil {
		t.Fatal(execErr)
	}
	if _, execErr := db.ExecContext(t.Context(), `INSERT INTO skills(id,key,name,category,scope) VALUES($1,'go','Go','backend','system')`, skill); execErr != nil {
		t.Fatal(execErr)
	}
	in := MatrixInput{Name: "Source", Visibility: "public", Levels: []LevelInput{{Key: "L1", Name: "First"}}, Groups: []GroupInput{{Key: "backend", Name: "Backend", Weight: 1}}, Skills: []MatrixSkillInput{{SkillID: skill, GroupKey: "backend", Weight: 1, Required: true}}, Requirements: []RequirementInput{{SkillID: skill, LevelKey: "L1", Description: "Original requirement", Weight: 1, Required: true}}}
	return owner, other, skill, in
}
func TestForkAndDuplicateCopyPublishedSnapshot(t *testing.T) {
	db := forkDB(t)
	owner, other, _, in := forkFixture(t, db)
	out, err := forkRun(t, db, owner, "createMatrix", "", in)
	source := forkMust(t, out, err)
	sourceID := source["id"].(string)
	version := source["version"].(domain.Result)
	versionID := version["id"].(string)
	out, err = forkRun(t, db, owner, "publishVersion", versionID, nil)
	forkMust(t, out, err)
	if _, deniedErr := forkRun(t, db, owner, "getUpstream", sourceID, nil); deniedErr == nil {
		t.Fatal("nonfork unexpectedly has upstream lineage")
	}
	for _, op := range []string{"forkMatrix", "duplicateMatrix"} {
		t.Run(op, func(t *testing.T) {
			out, err := forkRun(t, db, other, op, sourceID, map[string]string{"name": "My copy"})
			copied := forkMust(t, out, err)
			copiedVersion := copied["version"].(domain.Result)
			if copied["id"] == sourceID || copiedVersion["id"] == versionID || copiedVersion["status"] != "draft" {
				t.Fatalf("copy identity/state: %+v", copied)
			}
			var parent, parentVersion sql.NullString
			var visibility, scope, actualOwner string
			if queryErr := db.QueryRowContext(t.Context(), `SELECT parent_matrix_id,parent_version_id,visibility,scope,owner_id FROM matrices WHERE id=$1`, copied["id"]).Scan(&parent, &parentVersion, &visibility, &scope, &actualOwner); queryErr != nil {
				t.Fatal(queryErr)
			}
			if visibility != "private" || scope != "personal" || actualOwner != other {
				t.Fatal("copy did not become caller's private personal matrix")
			}
			if op == "forkMatrix" && (!parent.Valid || parent.String != sourceID || parentVersion.String != versionID) {
				t.Fatal("fork lost lineage")
			}
			if op == "duplicateMatrix" && (parent.Valid || parentVersion.Valid) {
				t.Fatal("duplicate retained lineage")
			}
			var originalRequirement, copiedRequirement, description string
			if queryErr := db.QueryRowContext(t.Context(), `SELECT id FROM requirements WHERE matrix_version_id=$1`, versionID).Scan(&originalRequirement); queryErr != nil {
				t.Fatal(queryErr)
			}
			if queryErr := db.QueryRowContext(t.Context(), `SELECT id,description FROM requirements WHERE matrix_version_id=$1`, copiedVersion["id"]).Scan(&copiedRequirement, &description); queryErr != nil {
				t.Fatal(queryErr)
			}
			if copiedRequirement == originalRequirement || description != "Original requirement" {
				t.Fatal("copy reused identity or changed semantics")
			}
		})
	}
}

func TestUpstreamReviewUsesSemanticDiffAndPreservesCustomDraft(t *testing.T) {
	db := forkDB(t)
	owner, other, _, in := forkFixture(t, db)
	out, err := forkRun(t, db, owner, "createMatrix", "", in)
	source := forkMust(t, out, err)
	sourceID := source["id"].(string)
	v1 := source["version"].(domain.Result)["id"].(string)
	out, err = forkRun(t, db, owner, "publishVersion", v1, nil)
	forkMust(t, out, err)
	out, err = forkRun(t, db, other, "forkMatrix", sourceID, map[string]string{"name": "Fork"})
	fork := forkMust(t, out, err)
	forkID := fork["id"].(string)
	out, err = forkRun(t, db, other, "getUpstream", forkID, nil)
	initial := forkMust(t, out, err)
	if initial["available"] != false {
		t.Fatal("new fork incorrectly has an upstream update")
	}
	out, err = forkRun(t, db, owner, "createVersion", sourceID, in)
	v2 := forkMust(t, out, err)["id"].(string)
	out, err = forkRun(t, db, owner, "publishVersion", v2, nil)
	forkMust(t, out, err)
	out, err = forkRun(t, db, other, "getUpstream", forkID, nil)
	same := forkMust(t, out, err)
	if same["available"] != true || len(same["changed_requirements"].([]domain.Result)) != 0 {
		t.Fatalf("regenerated IDs caused false semantic changes: %+v", same)
	}
	out, err = forkRun(t, db, other, "reviewUpstream", forkID, map[string]string{"action": "ignore", "upstream_version_id": v2})
	forkMust(t, out, err)
	out, err = forkRun(t, db, other, "getUpstream", forkID, nil)
	ignored := forkMust(t, out, err)
	if ignored["available"] != false || ignored["ignored"] != true {
		t.Fatalf("ignore not retained: %+v", ignored)
	}
	custom := in
	custom.Requirements = append([]RequirementInput(nil), in.Requirements...)
	custom.Requirements[0].Description = "My custom requirement"
	out, err = forkRun(t, db, other, "createVersion", forkID, custom)
	customID := forkMust(t, out, err)["id"].(string)
	changed := in
	changed.Requirements = append([]RequirementInput(nil), in.Requirements...)
	changed.Requirements[0].Description = "Upstream changed requirement"
	out, err = forkRun(t, db, owner, "createVersion", sourceID, changed)
	v3 := forkMust(t, out, err)["id"].(string)
	out, err = forkRun(t, db, owner, "publishVersion", v3, nil)
	forkMust(t, out, err)
	out, err = forkRun(t, db, other, "getUpstream", forkID, nil)
	diff := forkMust(t, out, err)
	if diff["available"] != true || len(diff["changed_requirements"].([]domain.Result)) != 1 {
		t.Fatalf("changed requirement missing: %+v", diff)
	}
	if _, deniedErr := forkRun(t, db, other, "reviewUpstream", forkID, map[string]string{"action": "apply", "upstream_version_id": v2}); deniedErr == nil {
		t.Fatal("accepted stale upstream version")
	}
	if _, deniedErr := forkRun(t, db, owner, "reviewUpstream", forkID, map[string]string{"action": "apply", "upstream_version_id": v3}); deniedErr == nil {
		t.Fatal("upstream owner wrote another user's private fork")
	}
	out, err = forkRun(t, db, other, "reviewUpstream", forkID, map[string]string{"action": "apply", "upstream_version_id": v3})
	applied := forkMust(t, out, err)
	newVersion := applied["version"].(domain.Result)
	if newVersion["id"] == customID || newVersion["status"] != "draft" {
		t.Fatal("apply reused custom draft or auto-published")
	}
	var preserved, newDescription, parentVersion string
	if queryErr := db.QueryRowContext(t.Context(), `SELECT description FROM requirements WHERE matrix_version_id=$1`, customID).Scan(&preserved); queryErr != nil {
		t.Fatal(queryErr)
	}
	if queryErr := db.QueryRowContext(t.Context(), `SELECT description FROM requirements WHERE matrix_version_id=$1`, newVersion["id"]).Scan(&newDescription); queryErr != nil {
		t.Fatal(queryErr)
	}
	if queryErr := db.QueryRowContext(t.Context(), `SELECT parent_version_id FROM matrices WHERE id=$1`, forkID).Scan(&parentVersion); queryErr != nil {
		t.Fatal(queryErr)
	}
	if preserved != "My custom requirement" || newDescription != "Upstream changed requirement" || parentVersion != v3 {
		t.Fatal("apply did not preserve old custom state and advance lineage")
	}
	out, err = forkRun(t, db, other, "getUpstream", forkID, nil)
	done := forkMust(t, out, err)
	if done["available"] != false {
		t.Fatal("applied update remained available")
	}
	if _, deniedErr := forkRun(t, db, other, "reviewUpstream", forkID, map[string]string{"action": "apply", "upstream_version_id": v3}); deniedErr == nil {
		t.Fatal("reapplied upstream created a duplicate draft")
	}
}

func TestDatabasePublishedContentCannotBeDemotedOrMoved(t *testing.T) {
	db := forkDB(t)
	owner, _, _, in := forkFixture(t, db)
	out, err := forkRun(t, db, owner, "createMatrix", "", in)
	source := forkMust(t, out, err)
	matrixID := source["id"].(string)
	v1 := source["version"].(domain.Result)["id"].(string)
	unusedGroup := ID()
	if _, execErr := db.ExecContext(t.Context(), `INSERT INTO competency_groups(id,matrix_version_id,key,name,weight) VALUES($1,$2,'unused','Unused',1)`, unusedGroup, v1); execErr != nil {
		t.Fatal(execErr)
	}
	out, err = forkRun(t, db, owner, "publishVersion", v1, nil)
	forkMust(t, out, err)
	out, err = forkRun(t, db, owner, "createVersion", matrixID, in)
	v2 := forkMust(t, out, err)["id"].(string)
	for _, tt := range []struct {
		name, query string
		args        []any
	}{
		{"demotion", `UPDATE matrix_versions SET status='draft' WHERE id=$1`, []any{v1}},
		{"published metadata mutation", `UPDATE matrix_versions SET number=99 WHERE id=$1`, []any{v1}},
		{"move old published child to draft", `UPDATE competency_groups SET matrix_version_id=$2 WHERE id=$1`, []any{unusedGroup, v2}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.ExecContext(t.Context(), tt.query, tt.args...); err == nil {
				t.Fatal("published state was mutable")
			}
		})
	}
	if err := goose.DownToContext(t.Context(), db, "../../../migrations", 0); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpContext(t.Context(), db, "../../../migrations"); err != nil {
		t.Fatal(err)
	}
}
