//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	adapter "github.com/team-screaner/screener-service/internal/adapter/postgres"
	"github.com/team-screaner/screener-service/internal/domain"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestEvidenceIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pg, err := tcpostgres.Run(ctx, "postgres:17-alpine", tcpostgres.WithDatabase("evidence"), tcpostgres.WithUsername("test"), tcpostgres.WithPassword("test"), tcpostgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if checkErr := pg.Terminate(cleanupCtx); checkErr != nil {
			t.Error(checkErr)
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
		if checkErr := db.Close(); checkErr != nil {
			t.Error(checkErr)
		}
	})
	if checkErr := goose.UpContext(ctx, db, "../../../migrations"); checkErr != nil {
		t.Fatal(checkErr)
	}
	user, other, skill := uuid.NewString(), uuid.NewString(), uuid.NewString()
	evidenceSQL(t, db, `INSERT INTO users(id,email,name,password_hash) VALUES($1,'one@test.invalid','One','x'),($2,'two@test.invalid','Two','x')`, user, other)
	evidenceSQL(t, db, `INSERT INTO skill_categories(key,name) VALUES('go','Go');INSERT INTO fact_types(key,name) VALUES('project','Project')`)
	evidenceSQL(t, db, `INSERT INTO skills(id,key,name,category,scope) VALUES($1,'go.channels','Channels','go','system')`, skill)
	ownReq := evidenceRequirement(t, db, user, skill, true)
	unassignedReq := evidenceRequirement(t, db, user, skill, false)
	session := domain.Actor{UserID: user, Kind: "session"}
	agent := domain.Actor{UserID: user, Kind: "agent"}
	call := func(actor domain.Actor, operation, id string, body any, query url.Values) (domain.Result, error) {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback() }()
		result, err := adapter.HandleEvidence(ctx, tx, domain.Command{Actor: actor, Operation: operation, ID: id, Body: raw, Query: query})
		if err != nil {
			return nil, err
		}
		if checkErr := tx.Commit(); checkErr != nil {
			return nil, checkErr
		}
		return result, nil
	}
	input := func(external string) map[string]any {
		return map[string]any{"external_id": external, "title": "Shipped channels", "description": "Implemented backpressure", "fact_type": "project", "source": "github", "source_url": "https://github.com/example/repo/pull/1", "matches": []any{map[string]any{"skill_key": "go.channels", "requirement_id": ownReq, "confidence": 0.9, "reason": "Demonstrated bounded workers"}}}
	}
	t.Run("manual accepted and agent suggested", func(t *testing.T) {
		manual := input("")
		manual["source"] = "manual"
		got, err := call(session, "createEvidence", "", manual, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got["status"] != "accepted" || got["version"] != float64(1) {
			t.Fatalf("manual=%v", got)
		}
		suggested, err := call(agent, "createEvidence", "", input("agent-one"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if suggested["status"] != "suggested" {
			t.Fatalf("agent=%v", suggested)
		}
	})
	t.Run("batch dedupe preserves review and conflicts on changed import", func(t *testing.T) {
		batch := map[string]any{"period": "2026-Q3", "items": []any{input("import-one")}}
		got, err := call(agent, "batchEvidence", "", batch, nil)
		if err != nil {
			t.Fatal(err)
		}
		first := evidenceItems(t, got)[0]
		id := first["id"].(string)
		if first["status"] != "suggested" || first["period_from"] != "2026-07-01" || first["period_to"] != "2026-09-30" {
			t.Fatalf("batch=%v", first)
		}
		reviewed, err := call(session, "reviewEvidence", id, map[string]any{"status": "accepted", "version": 1, "title": "Reviewed title"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if reviewed["version"] != float64(2) {
			t.Fatalf("review=%v", reviewed)
		}
		replay, err := call(agent, "batchEvidence", "", batch, nil)
		if err != nil {
			t.Fatal(err)
		}
		item := evidenceItems(t, replay)[0]
		if item["id"] != id || item["status"] != "accepted" || item["title"] != "Reviewed title" {
			t.Fatalf("replay overwrote review: %v", item)
		}
		changed := input("import-one")
		changed["title"] = "Changed source title"
		_, err = call(agent, "batchEvidence", "", map[string]any{"period": "2026-Q3", "items": []any{changed}}, nil)
		evidenceStatus(t, err, 409)
		_, err = call(session, "reviewEvidence", id, map[string]any{"status": "rejected", "version": 1}, nil)
		evidenceStatus(t, err, 409)
		_, err = call(agent, "reviewEvidence", id, map[string]any{"status": "rejected", "version": 2}, nil)
		evidenceStatus(t, err, 403)
		_, err = call(domain.Actor{UserID: other, Kind: "session"}, "reviewEvidence", id, map[string]any{"status": "accepted", "version": 2}, nil)
		evidenceStatus(t, err, 404)
	})
	t.Run("invalid batch rolls back all items", func(t *testing.T) {
		bad := input("batch-bad")
		bad["matches"] = []any{map[string]any{"skill_key": "go.channels", "requirement_id": unassignedReq, "confidence": 0.5, "reason": "not assigned"}}
		_, err := call(agent, "batchEvidence", "", map[string]any{"items": []any{input("rollback-first"), bad}}, nil)
		evidenceStatus(t, err, 400)
		var count int
		if checkErr := db.QueryRowContext(ctx, `SELECT count(*) FROM evidence WHERE external_id='rollback-first'`).Scan(&count); checkErr != nil {
			t.Fatal(checkErr)
		}
		if count != 0 {
			t.Fatal("partial batch committed")
		}
	})
	t.Run("validation", func(t *testing.T) {
		cases := []struct {
			name   string
			change func(map[string]any)
		}{
			{"source", func(x map[string]any) { x["source"] = "dropbox" }},
			{"fact", func(x map[string]any) { x["fact_type"] = "unknown" }},
			{"url", func(x map[string]any) { x["source_url"] = "javascript:alert(1)" }},
			{"dates", func(x map[string]any) { x["period_from"] = "2026-09-30"; x["period_to"] = "2026-07-01" }},
			{"confidence", func(x map[string]any) {
				x["matches"] = []any{map[string]any{"skill_key": "go.channels", "confidence": 1.1, "reason": "why"}}
			}},
			{"reason", func(x map[string]any) {
				x["matches"] = []any{map[string]any{"skill_key": "go.channels", "confidence": 0.5, "reason": " "}}
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				x := input("")
				tc.change(x)
				_, err := call(session, "createEvidence", "", x, nil)
				evidenceStatus(t, err, 400)
			})
		}
		_, err := call(agent, "batchEvidence", "", map[string]any{"items": []any{input("")}}, nil)
		evidenceStatus(t, err, 400)
		_, err = call(agent, "batchEvidence", "", map[string]any{"period": "2026-Q9", "items": []any{input("invalid-quarter")}}, nil)
		evidenceStatus(t, err, 400)
	})
	t.Run("skill visibility and requirement alignment", func(t *testing.T) {
		hidden, organization, orgSkill, otherSkill := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
		evidenceSQL(t, db, `INSERT INTO skills(id,key,name,category,scope,owner_id) VALUES($1,'private.go','Private','go','personal',$2)`, hidden, other)
		evidenceSQL(t, db, `INSERT INTO organizations(id,name) VALUES($1,'Org')`, organization)
		evidenceSQL(t, db, `INSERT INTO skills(id,key,name,category,scope,owner_id) VALUES($1,'org.go','Org skill','go','organization',$2)`, orgSkill, organization)
		evidenceSQL(t, db, `INSERT INTO skills(id,key,name,category,scope) VALUES($1,'go.other','Other skill','go','system')`, otherSkill)
		for _, key := range []string{"private.go", "org.go"} {
			x := input("")
			x["matches"] = []any{map[string]any{"skill_key": key, "confidence": 0.5, "reason": "match"}}
			_, err := call(session, "createEvidence", "", x, nil)
			evidenceStatus(t, err, 400)
		}
		evidenceSQL(t, db, `INSERT INTO organization_members(organization_id,user_id,role) VALUES($1,$2,'member')`, organization, user)
		x := input("org-visible")
		x["matches"] = []any{map[string]any{"skill_key": "org.go", "confidence": 0.5, "reason": "match"}}
		if _, err := call(session, "createEvidence", "", x, nil); err != nil {
			t.Fatal(err)
		}
		x = input("")
		x["matches"] = []any{map[string]any{"skill_key": "go.other", "requirement_id": ownReq, "confidence": 0.5, "reason": "wrong skill"}}
		_, err := call(session, "createEvidence", "", x, nil)
		evidenceStatus(t, err, 400)
	})
	t.Run("concurrent imports converge without overwriting", func(t *testing.T) {
		type answer struct {
			result domain.Result
			err    error
		}
		results := make(chan answer, 2)
		batch := map[string]any{"items": []any{input("concurrent-source")}}
		for range 2 {
			go func() { result, err := call(agent, "batchEvidence", "", batch, nil); results <- answer{result, err} }()
		}
		one, two := <-results, <-results
		if one.err != nil || two.err != nil {
			t.Fatalf("concurrent errors: %v; %v", one.err, two.err)
		}
		if evidenceItems(t, one.result)[0]["id"] != evidenceItems(t, two.result)[0]["id"] {
			t.Fatal("concurrent import duplicated record")
		}
	})
	t.Run("store idempotency replay and actor boundaries", func(t *testing.T) {
		store := adapter.New(db)
		body, err := json.Marshal(input("store-idempotency"))
		if err != nil {
			t.Fatal(err)
		}
		command := domain.Command{Operation: "createEvidence", Method: "POST", Path: "/api/v1/evidence", Key: "same-key", Actor: session, Body: body}
		first, err := store.Execute(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		second, err := store.Execute(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		if first["id"] != second["id"] {
			t.Fatal("same key duplicated evidence")
		}
		command.Body = []byte(`{"title":"changed"}`)
		_, err = store.Execute(ctx, command)
		evidenceStatus(t, err, 422)
		var count int
		if checkErr := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE actor_id=$1 AND operation='createEvidence'`, user).Scan(&count); checkErr != nil {
			t.Fatal(checkErr)
		}
		if count != 1 {
			t.Fatalf("replay audited twice: count=%d", count)
		}
	})

	t.Run("list owner isolation pagination and matches", func(t *testing.T) {
		got, err := call(session, "listEvidence", "", nil, url.Values{"limit": {"1"}})
		if err != nil {
			t.Fatal(err)
		}
		items := evidenceItems(t, got)
		if len(items) != 1 || got["next_cursor"] == "" {
			t.Fatalf("first page=%v", got)
		}
		if len(items[0]["matches"].([]any)) != 1 {
			t.Fatalf("missing matches: %v", items)
		}
		next, err := call(session, "listEvidence", "", nil, url.Values{"limit": {"1"}, "cursor": {got["next_cursor"].(string)}})
		if err != nil {
			t.Fatal(err)
		}
		if evidenceItems(t, next)[0]["id"] == items[0]["id"] {
			t.Fatal("page repeated item")
		}
		isolated, err := call(domain.Actor{UserID: other, Kind: "session"}, "listEvidence", "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(evidenceItems(t, isolated)) != 0 {
			t.Fatal("leaked other user's evidence")
		}
	})
}

func evidenceItems(t *testing.T, result domain.Result) []domain.Result {
	t.Helper()
	raw, err := json.Marshal(result["items"])
	if err != nil {
		t.Fatal(err)
	}
	var items []domain.Result
	if checkErr := json.Unmarshal(raw, &items); checkErr != nil {
		t.Fatal(checkErr)
	}
	return items
}
func evidenceStatus(t *testing.T, err error, status int) {
	t.Helper()
	var e *domain.Error
	if !errors.As(err, &e) || e.Status != status {
		t.Fatalf("error=%v, want HTTP %d", err, status)
	}
}
func evidenceSQL(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), q, args...); err != nil {
		t.Fatal(err)
	}
}
func evidenceRequirement(t *testing.T, db *sql.DB, user, skill string, assign bool) string {
	t.Helper()
	matrix, version, level, group, ms, req := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	evidenceSQL(t, db, `INSERT INTO matrices(id,name,scope,visibility) VALUES($1,'Backend','system','public')`, matrix)
	evidenceSQL(t, db, `INSERT INTO matrix_versions(id,matrix_id,number,status) VALUES($1,$2,1,'draft')`, version, matrix)
	evidenceSQL(t, db, `INSERT INTO levels(id,matrix_version_id,key,name,position) VALUES($1,$2,'senior','Senior',1)`, level, version)
	evidenceSQL(t, db, `INSERT INTO competency_groups(id,matrix_version_id,key,name,weight) VALUES($1,$2,'go','Go',1)`, group, version)
	evidenceSQL(t, db, `INSERT INTO matrix_skills(id,matrix_version_id,skill_id,group_id,weight,required,active,position) VALUES($1,$2,$3,$4,1,true,true,1)`, ms, version, skill, group)
	evidenceSQL(t, db, `INSERT INTO requirements(id,matrix_version_id,matrix_skill_id,level_id,description,required,critical,weight) VALUES($1,$2,$3,$4,'Use channels',true,false,1)`, req, version, ms, level)
	evidenceSQL(t, db, `UPDATE matrix_versions SET status='published' WHERE id=$1`, version)
	if assign {
		evidenceSQL(t, db, `INSERT INTO user_matrices(id,user_id,matrix_version_id,current_level_id,target_level_id) VALUES($1,$2,$3,$4,$4)`, uuid.NewString(), user, version, level)
	}
	return req
}
