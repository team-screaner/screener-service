//go:build integration

package http_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	transport "github.com/team-screaner/screener-service/internal/adapter/http"
	tc "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func setup(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pg, err := tc.Run(ctx, "postgres:17-alpine", tc.WithDatabase("screener"), tc.WithUsername("screener"), tc.WithPassword("screener"), tc.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if cleanupErr := pg.Terminate(cleanup); cleanupErr != nil {
			t.Log(cleanupErr)
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
	t.Cleanup(func() { _ = db.Close() })
	if err = goose.Up(db, "../../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err = goose.DownTo(db, "../../../migrations", 0); err != nil {
		t.Fatal(err)
	}
	if err = goose.Up(db, "../../../migrations"); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(transport.New(db))
	t.Cleanup(s.Close)
	return s, db
}
func call(t *testing.T, s *httptest.Server, method, path, token, key string, body any, want int) map[string]any {
	t.Helper()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), method, s.URL+"/api/v1"+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	var out map[string]any
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("JSON: %v %s", err, raw)
	}
	return out
}
func register(t *testing.T, s *httptest.Server, email string) string {
	t.Helper()
	r := call(t, s, "POST", "/auth/register", "", email, map[string]any{"email": email, "password": "safe-long-password", "name": "Test"}, 200)
	return r["token"].(string)
}
func TestAccountAndScopedToken(t *testing.T) {
	s, _ := setup(t)
	token := register(t, s, "alice@example.com")
	me := call(t, s, "GET", "/me", token, "", nil, 200)
	if me["email"] != "alice@example.com" {
		t.Fatalf("wrong user: %v", me)
	}
	call(t, s, "GET", "/me", "", "", nil, 401)
	scoped := call(t, s, "POST", "/tokens", token, "agent", map[string]any{"name": "Quarter import", "scopes": []string{"matrix:read", "evidence:write"}, "expires_in_days": 7}, 200)
	call(t, s, "POST", "/organizations", scoped["token"].(string), "org", map[string]any{"name": "Denied"}, 403)
	call(t, s, "DELETE", "/tokens/"+scoped["id"].(string), token, "revoke", nil, 200)
	call(t, s, "GET", "/me", scoped["token"].(string), "", nil, 401)
}

func TestPublishedVersionsAndPrivateOwnership(t *testing.T) {
	s, db := setup(t)
	token := register(t, s, "matrix@example.com")
	other := register(t, s, "other@example.com")
	skillID := "01999999-0000-7000-8000-000000000001"
	if _, err := db.ExecContext(t.Context(), `INSERT INTO skill_categories(key,name) VALUES('architecture','Architecture'); INSERT INTO skills(id,key,name,category,scope) VALUES('01999999-0000-7000-8000-000000000001','system-design','System Design','architecture','system')`); err != nil {
		t.Fatal(err)
	}
	in := map[string]any{"name": "Custom track", "levels": []any{map[string]any{"key": "L1", "name": "First"}, map[string]any{"key": "L2", "name": "Next"}}, "groups": []any{map[string]any{"key": "architecture", "name": "Architecture", "weight": 1}}, "skills": []any{map[string]any{"skill_id": skillID, "group_key": "architecture", "weight": 1, "required": true, "active": true}}, "requirements": []any{map[string]any{"skill_id": skillID, "level_key": "L2", "description": "Design a service with documented tradeoffs", "weight": 1, "required": true, "critical": true}}}
	matrix := call(t, s, "POST", "/matrices", token, "matrix", in, 200)
	id := matrix["id"].(string)
	v := matrix["version"].(map[string]any)
	vid := v["id"].(string)
	call(t, s, "GET", "/matrices/"+id, other, "", nil, 404)
	call(t, s, "POST", "/matrix-versions/"+vid+"/publish", token, "publish", nil, 200)
	call(t, s, "PUT", "/matrix-versions/"+vid, token, "edit", map[string]any{"expected_version": 1, "matrix": in}, 409)
	next := call(t, s, "POST", "/matrices/"+id+"/versions", token, "next", in, 200)
	if next["id"] == vid {
		t.Fatal("version identity reused")
	}
	old := call(t, s, "GET", "/matrix-versions/"+vid, token, "", nil, 200)
	if old["status"] != "published" {
		t.Fatalf("old version changed: %v", old)
	}
}
