//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	adapter "github.com/team-screaner/screener-service/internal/adapter/postgres"
	workbook "github.com/team-screaner/screener-service/internal/adapter/xlsx"
	"github.com/team-screaner/screener-service/internal/domain"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/xuri/excelize/v2"
)

func TestImportExportIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pg, err := tcpostgres.Run(ctx, "postgres:17-alpine", tcpostgres.WithDatabase("importexport"), tcpostgres.WithUsername("test"), tcpostgres.WithPassword("test"), tcpostgres.BasicWaitStrategies())
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
	user, other := uuid.NewString(), uuid.NewString()
	evidenceSQL(t, db, `INSERT INTO users(id,email,name,password_hash) VALUES($1,'import@test.invalid','Import','x'),($2,'other@test.invalid','Other','x')`, user, other)
	evidenceSQL(t, db, `INSERT INTO skill_categories(key,name) VALUES('engineering_core','Engineering core');INSERT INTO fact_types(key,name) VALUES('project','Project')`)
	call := func(actor, operation string, body any, query url.Values) (domain.Result, error) {
		raw, callErr := json.Marshal(body)
		if callErr != nil {
			return nil, callErr
		}
		tx, callErr := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
		if callErr != nil {
			return nil, callErr
		}
		defer func() { _ = tx.Rollback() }()
		got, callErr := adapter.HandleImportExport(ctx, tx, domain.Command{Operation: operation, Actor: domain.Actor{UserID: actor, Kind: "session"}, Body: raw, Query: query})
		if callErr != nil {
			return nil, callErr
		}
		if checkErr := tx.Commit(); checkErr != nil {
			return nil, checkErr
		}
		return got, nil
	}
	doc := workbook.Document{Name: "Backend", Levels: []string{"Junior", "Senior"}, Rows: []workbook.Row{{Category: "Concurrency", Skill: "Worker pools", Requirements: map[string]string{"Junior": "Use channels", "Senior": "=literal requirement"}}}}
	data, err := workbook.Export(doc)
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"file_base64": base64.StdEncoding.EncodeToString(data)}
	preview, err := call(user, "importXLSX", payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview["valid"] != true {
		t.Fatalf("preview=%v", preview)
	}
	var count int
	if checkErr := db.QueryRowContext(ctx, `SELECT count(*) FROM matrices`).Scan(&count); checkErr != nil {
		t.Fatal(checkErr)
	}
	if count != 0 {
		t.Fatal("preview created matrix")
	}
	payload["confirm"] = true
	payload["name"] = "Imported Backend"
	created, err := call(user, "importXLSX", payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	var normalized domain.Result
	if checkErr := json.Unmarshal(encoded, &normalized); checkErr != nil {
		t.Fatal(checkErr)
	}
	matrix := normalized["id"].(string)
	version := normalized["version"].(map[string]any)["id"].(string)
	var scope, visibility, status string
	if checkErr := db.QueryRowContext(ctx, `SELECT scope,visibility,status FROM matrices WHERE id=$1`, matrix).Scan(&scope, &visibility, &status); checkErr != nil {
		t.Fatal(checkErr)
	}
	if scope != "personal" || visibility != "private" || status != "draft" {
		t.Fatalf("import=%s/%s/%s", scope, visibility, status)
	}
	if _, repeatErr := call(user, "importXLSX", payload, nil); repeatErr != nil {
		t.Fatal(repeatErr)
	}
	if checkErr := db.QueryRowContext(ctx, `SELECT count(*) FROM skills WHERE scope='personal' AND owner_id=$1`, user).Scan(&count); checkErr != nil {
		t.Fatal(checkErr)
	}
	if count != 1 {
		t.Fatalf("natural-key skill duplicate: %d", count)
	}
	exported, err := call(user, "exportXLSX", nil, url.Values{"matrix_version_id": {version}})
	if err != nil {
		t.Fatal(err)
	}
	book := exportedWorkbook(t, exported)
	got, err := workbook.Preview(bytes.NewReader(book), "", workbook.Mapping{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows[0].Requirements["Senior"] != "=literal requirement" || got.Levels[0] != "Junior" {
		t.Fatalf("roundtrip=%v", got)
	}
	_, err = call(other, "exportXLSX", nil, url.Values{"matrix_version_id": {version}})
	evidenceStatus(t, err, 404)
	t.Run("personal export includes effective overrides assessment and only owned evidence", func(t *testing.T) {
		evidenceSQL(t, db, `UPDATE matrix_versions SET status='published' WHERE id=$1`, version)
		var level, requirement, skill string
		if checkErr := db.QueryRowContext(ctx, `SELECT l.id,r.id,ms.skill_id FROM levels l JOIN requirements r ON r.level_id=l.id JOIN matrix_skills ms ON ms.id=r.matrix_skill_id WHERE l.matrix_version_id=$1 ORDER BY l.position LIMIT 1`, version).Scan(&level, &requirement, &skill); checkErr != nil {
			t.Fatal(checkErr)
		}
		um, assessment, ownEvidence, foreignEvidence := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
		evidenceSQL(t, db, `INSERT INTO user_matrices(id,user_id,matrix_version_id,current_level_id,target_level_id) VALUES($1,$2,$3,$4,$4)`, um, user, version, level)
		evidenceSQL(t, db, `INSERT INTO personal_overrides(user_matrix_id,requirement_id,description,reason) VALUES($1,$2,'Effective requirement','Personal context')`, um, requirement)
		evidenceSQL(t, db, `INSERT INTO assessments(id,user_id,user_matrix_id,matrix_version_id,period,type) VALUES($1,$2,$3,$4,'2026-Q3','self')`, assessment, user, um, version)
		evidenceSQL(t, db, `INSERT INTO assessment_items(assessment_id,requirement_id,score,status,comment,requirement_snapshot) VALUES($1,$2,3,'assessed','=literal comment','Original snapshot')`, assessment, requirement)
		for _, item := range []struct{ id, user, title string }{{ownEvidence, user, "=owned evidence"}, {foreignEvidence, other, "FOREIGN SECRET"}} {
			evidenceSQL(t, db, `INSERT INTO evidence(id,user_id,title,description,fact_type,source,status,created_by) VALUES($1,$2,$3,'Description','project','manual','accepted',$2)`, item.id, item.user, item.title)
			evidenceSQL(t, db, `INSERT INTO evidence_matches(id,evidence_id,skill_id,requirement_id,confidence,reason,created_by) VALUES($1,$2,$3,$4,1,'Reason',$5)`, uuid.NewString(), item.id, skill, requirement, item.user)
		}
		result, err := call(user, "exportXLSX", nil, url.Values{"user_matrix_id": {um}})
		if err != nil {
			t.Fatal(err)
		}
		resultData := exportedWorkbook(t, result)
		effective, err := workbook.Preview(bytes.NewReader(resultData), "", workbook.Mapping{})
		if err != nil {
			t.Fatal(err)
		}
		if effective.Rows[0].Requirements["Junior"] != "Effective requirement" {
			t.Fatal("override missing")
		}
		f, err := excelize.OpenReader(bytes.NewReader(resultData))
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if checkErr := f.Close(); checkErr != nil {
				t.Error(checkErr)
			}
		}()
		if len(f.GetSheetList()) != 3 {
			t.Fatalf("sheets=%v", f.GetSheetList())
		}
		evidenceRows, err := f.GetRows("Evidence")
		if err != nil {
			t.Fatal(err)
		}
		all, err := json.Marshal(evidenceRows)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(all, []byte("=owned evidence")) || bytes.Contains(all, []byte("FOREIGN SECRET")) {
			t.Fatalf("evidence sheet=%s", all)
		}
		for _, sheet := range f.GetSheetList() {
			rows, sheetErr := f.GetRows(sheet)
			if sheetErr != nil {
				t.Fatal(sheetErr)
			}
			for y, row := range rows {
				for x := range row {
					cell, sheetErr := excelize.CoordinatesToCellName(x+1, y+1)
					if sheetErr != nil {
						t.Fatal(sheetErr)
					}
					formula, sheetErr := f.GetCellFormula(sheet, cell)
					if sheetErr != nil {
						t.Fatal(sheetErr)
					}
					if formula != "" {
						t.Fatalf("formula exported in %s!%s", sheet, cell)
					}
				}
			}
		}

		evidenceSQL(t, db, `UPDATE matrices SET name=' Assessment ' WHERE id=$1`, matrix)
		reserved, reservedErr := call(user, "exportXLSX", nil, url.Values{"user_matrix_id": {um}})
		if reservedErr != nil {
			t.Fatal(reservedErr)
		}
		reservedData := exportedWorkbook(t, reserved)
		names, namesErr := workbook.Sheets(bytes.NewReader(reservedData))
		if namesErr != nil {
			t.Fatal(namesErr)
		}
		if len(names) != 3 || names[0] != "Matrix" {
			t.Fatalf("reserved title corrupted workbook sheets: %v", names)
		}
		_, err = call(other, "exportXLSX", nil, url.Values{"user_matrix_id": {um}})
		if err == nil {
			t.Fatal("foreign personal export allowed")
		}
	})
	t.Run("invalid base64 rejected", func(t *testing.T) {
		_, err := call(user, "importXLSX", map[string]any{"file_base64": "not-base64!"}, nil)
		evidenceStatus(t, err, 400)
	})
}

func exportedWorkbook(t *testing.T, result domain.Result) []byte {
	t.Helper()
	if result["content_type"] != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("type=%v", result)
	}
	data, err := base64.StdEncoding.DecodeString(result["content_base64"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
