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

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	adapter "github.com/team-screaner/screener-service/internal/adapter/postgres"
	"github.com/team-screaner/screener-service/internal/domain"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func catalogDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pg, err := tcpg.Run(ctx, "postgres:17-alpine", tcpg.WithDatabase("catalog"), tcpg.WithUsername("test"), tcpg.WithPassword("test"), tcpg.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if cleanupErr := pg.Terminate(cleanup); cleanupErr != nil {
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
	if err := goose.Up(db, "../../../migrations"); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestCatalogSeedIsCompleteAndIdempotent(t *testing.T) {
	db := catalogDB(t)
	for range 2 {
		if err := adapter.Seed(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	for table, want := range map[string]int{"skills": 236, "fact_types": 24, "matrices": 8, "matrix_versions": 8, "levels": 24} {
		var got int
		if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s count=%d want=%d", table, got, want)
		}
	}
	var unpublished int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM matrix_versions WHERE status<>'published'").Scan(&unpublished); err != nil {
		t.Fatal(err)
	}
	if unpublished != 0 {
		t.Fatal("seeded versions must be published")
	}
}
func registerCatalogActor(t *testing.T, s *adapter.Store, email string) domain.Actor {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": "a-long-password-123", "name": email})
	out, err := s.Execute(t.Context(), domain.Command{Operation: "register", Method: "POST", Path: "/api/v1/auth/register", Key: email, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := s.Authenticate(t.Context(), out["token"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return actor
}
func TestOrganizationAndDictionaryIsolation(t *testing.T) {
	db := catalogDB(t)
	if err := adapter.Seed(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	s := adapter.New(db)
	owner := registerCatalogActor(t, s, "owner@test.example")
	member := registerCatalogActor(t, s, "member@test.example")
	outsider := registerCatalogActor(t, s, "outsider@test.example")
	run := func(actor domain.Actor, op, method, id string, body any) (domain.Result, error) {
		b, _ := json.Marshal(body)
		return s.Execute(t.Context(), domain.Command{Actor: actor, Operation: op, Method: method, Path: op + "/" + id, ID: id, Key: adapter.ID(), Body: b})
	}
	org, err := run(owner, "createOrganization", "POST", "", map[string]string{"name": "Engineering"})
	if err != nil {
		t.Fatal(err)
	}
	oid := org["id"].(string)
	if _, err = run(owner, "addMember", "POST", oid, map[string]string{"user_id": member.UserID, "role": "admin"}); err != nil {
		t.Fatal(err)
	}
	if _, err = run(member, "addMember", "POST", oid, map[string]string{"user_id": owner.UserID, "role": "member"}); !isCatalogStatus(err, 403) {
		t.Fatalf("admin modified owner: %v", err)
	}
	if _, err = run(outsider, "listMembers", "GET", oid, nil); !isCatalogStatus(err, 403) {
		t.Fatalf("outsider listed members: %v", err)
	}
	skill, err := run(owner, "createSkill", "POST", "", map[string]any{"key": "team-concurrency", "name": "Concurrency", "scope": "organization", "organization_id": oid, "category": "backend", "signals": []string{"Race-free execution"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = run(outsider, "deactivateSkill", "PATCH", skill["id"].(string), map[string]bool{"active": false}); !isCatalogStatus(err, 403) {
		t.Fatalf("outsider mutated skill: %v", err)
	}
	if _, err = run(member, "deactivateSkill", "PATCH", skill["id"].(string), map[string]bool{"active": false}); err != nil {
		t.Fatal(err)
	}
	personal, err := run(owner, "createSkill", "POST", "", map[string]string{"key": "private-skill", "name": "Private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = run(member, "deactivateSkill", "PATCH", personal["id"].(string), map[string]bool{"active": false}); !isCatalogStatus(err, 403) {
		t.Fatalf("organization leaked personal ownership: %v", err)
	}
	if _, err = run(owner, "deactivateSkill", "PATCH", personal["id"].(string), map[string]any{"key": "renamed", "active": false}); !isCatalogStatus(err, 400) {
		t.Fatalf("skill key mutation accepted: %v", err)
	}
}

func TestSkillPaginationBindsCallerAndFilters(t *testing.T) {
	db := catalogDB(t)
	if err := adapter.Seed(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	store := adapter.New(db)
	owner := registerCatalogActor(t, store, "paging-owner@test.example")
	other := registerCatalogActor(t, store, "paging-other@test.example")
	list := func(actor domain.Actor, query url.Values) (domain.Result, error) {
		return store.Execute(t.Context(), domain.Command{Actor: actor, Operation: "listSkills", Method: "GET", Path: "/api/v1/skills", Query: query})
	}
	query := url.Values{"limit": {"7"}, "category": {"backend"}}
	first, err := list(owner, query)
	if err != nil {
		t.Fatal(err)
	}
	cursor, ok := first["next_cursor"].(string)
	if !ok || cursor == "" {
		t.Fatal("missing continuation cursor")
	}
	query.Set("cursor", cursor)
	if _, err := list(other, query); !isCatalogStatus(err, 400) {
		t.Fatalf("cursor accepted for different actor: %v", err)
	}
	query.Set("category", "qa")
	if _, err := list(owner, query); !isCatalogStatus(err, 400) {
		t.Fatalf("cursor accepted for changed filter: %v", err)
	}
	query.Set("category", "backend")
	query.Del("cursor")
	seen := map[string]bool{}
	for {
		page, err := list(owner, query)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page["items"].([]domain.Result) {
			id := item["id"].(string)
			if seen[id] {
				t.Fatalf("duplicate item %s", id)
			}
			seen[id] = true
		}
		cursor := page["next_cursor"].(string)
		if cursor == "" {
			break
		}
		query.Set("cursor", cursor)
	}
	if len(seen) != 52 {
		t.Fatalf("got %d backend skills, want 52", len(seen))
	}
	query.Set("limit", "101")
	query.Del("cursor")
	if _, err := list(owner, query); !isCatalogStatus(err, 400) {
		t.Fatalf("unbounded page accepted: %v", err)
	}
}

func TestCatalogReadQueryPlans(t *testing.T) {
	db := catalogDB(t)
	if err := adapter.Seed(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	store := adapter.New(db)
	actor := registerCatalogActor(t, store, "plans@test.example")
	org, err := store.Execute(t.Context(), domain.Command{Actor: actor, Operation: "createOrganization", Method: "POST", Path: "/api/v1/organizations", Key: "plan-org", Body: []byte(`{"name":"Plan fixture"}`)})
	if err != nil {
		t.Fatal(err)
	}
	organization := org["id"].(string)
	if _, err := db.ExecContext(t.Context(), `ANALYZE skills; ANALYZE organization_members; ANALYZE organizations; ANALYZE users; ANALYZE fact_types; ANALYZE evidence_signals`); err != nil {
		t.Fatal(err)
	}
	queries := []struct {
		name, sql string
		args      []any
	}{
		{"visible skills", `EXPLAIN (ANALYZE, BUFFERS) SELECT to_jsonb(s)||jsonb_build_object('signals',COALESCE((SELECT jsonb_agg(signal ORDER BY signal) FROM evidence_signals WHERE skill_id=s.id),'[]'::jsonb)) FROM skills s WHERE (s.scope='system' OR (s.scope='personal' AND s.owner_id=$1) OR (s.scope='organization' AND EXISTS(SELECT 1 FROM organization_members m WHERE m.organization_id=s.owner_id AND m.user_id=$1))) AND ($2::uuid IS NULL OR s.id>$2::uuid) AND ($3='' OR s.category=$3) AND ($4='' OR s.scope=$4) AND ($5::boolean IS NULL OR s.active=$5::boolean) AND ($6::uuid IS NULL OR (s.scope='organization' AND s.owner_id=$6::uuid)) ORDER BY s.id LIMIT $7`, []any{actor.UserID, nil, "", "", nil, nil, 51}},
		{"organizations", `EXPLAIN (ANALYZE, BUFFERS) SELECT to_jsonb(o)||jsonb_build_object('role',m.role) FROM organizations o JOIN organization_members m ON m.organization_id=o.id WHERE m.user_id=$1 AND ($2::uuid IS NULL OR o.id>$2::uuid) ORDER BY o.id LIMIT $3`, []any{actor.UserID, nil, 51}},
		{"members", `EXPLAIN (ANALYZE, BUFFERS) SELECT jsonb_build_object('user_id',m.user_id,'name',u.name,'role',m.role) FROM organization_members m JOIN users u ON u.id=m.user_id WHERE m.organization_id=$1 AND ($2::uuid IS NULL OR m.user_id>$2::uuid) ORDER BY m.user_id LIMIT $3`, []any{organization, nil, 51}},
		{"fact types", `EXPLAIN (ANALYZE, BUFFERS) SELECT to_jsonb(f) FROM fact_types f WHERE key>$1 ORDER BY key LIMIT $2`, []any{"", 51}},
	}
	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			rows, err := db.QueryContext(t.Context(), query.sql, query.args...)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if closeErr := rows.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			}()
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					t.Fatal(err)
				}
				t.Log(query.name, line)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func isCatalogStatus(err error, want int) bool {
	var e *domain.Error
	return errors.As(err, &e) && e.Status == want
}
