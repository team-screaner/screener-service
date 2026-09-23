package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	workbook "github.com/team-screaner/screener-service/internal/adapter/xlsx"
	"github.com/team-screaner/screener-service/internal/domain"
	"github.com/xuri/excelize/v2"
)

// ImportInput carries a workbook and a separate confirmation flag.
type ImportInput struct {
	FileBase64 string           `json:"file_base64"`
	Sheet      string           `json:"sheet"`
	Mapping    workbook.Mapping `json:"mapping"`
	Name       string           `json:"name"`
	Confirm    bool             `json:"confirm"`
}

// HandleImportExport uses the caller's transaction. Exports require repeatable
// read isolation so matrix, overrides and snapshots describe one database view.
func HandleImportExport(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	switch c.Operation {
	case "importXLSX":
		return importWorkbook(ctx, tx, c)
	case "exportXLSX":
		return exportWorkbook(ctx, tx, c)
	default:
		return nil, domain.NotFound()
	}
}

func importWorkbook(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	in, err := decode[ImportInput](c.Body)
	if err != nil {
		return nil, err
	}
	if len(in.FileBase64) > base64.StdEncoding.EncodedLen(workbook.MaxFileBytes) {
		return nil, domain.Invalid("Workbook exceeds 10 MiB")
	}
	data, err := base64.StdEncoding.DecodeString(in.FileBase64)
	if err != nil {
		return nil, domain.Invalid("Invalid workbook base64")
	}
	sheets, err := workbook.Sheets(bytes.NewReader(data))
	if err != nil {
		return nil, domain.Invalid("Invalid or oversized XLSX workbook")
	}
	doc, err := workbook.Preview(bytes.NewReader(data), in.Sheet, in.Mapping)
	if err != nil {
		return nil, domain.Invalid(err.Error())
	}
	if strings.TrimSpace(in.Name) != "" {
		doc.Name = strings.TrimSpace(in.Name)
	}
	if checkErr := validateImportDocument(doc); checkErr != nil {
		return nil, checkErr
	}
	if !in.Confirm {
		return domain.Result{"sheets": sheets, "preview": doc, "valid": true}, nil
	}
	matrix, err := importMatrixInput(ctx, tx, c.Actor.UserID, doc)
	if err != nil {
		return nil, err
	}
	return createMatrixInput(ctx, tx, c.Actor.UserID, matrix, "", "")
}

func validateImportDocument(doc workbook.Document) error {
	if strings.TrimSpace(doc.Name) == "" || len(doc.Name) > 200 {
		return domain.Invalid("Matrix name required (max 200 bytes)")
	}
	if len(doc.Rows) > 500 {
		return domain.Invalid("Matrix import supports at most 500 skills")
	}
	groups := make(map[string]bool)
	requirements := 0
	for _, row := range doc.Rows {
		groups[row.Category] = true
		for _, description := range row.Requirements {
			if len(description) > 10000 {
				return domain.Invalid("Requirement text exceeds 10000 bytes")
			}
			if strings.TrimSpace(description) != "" {
				requirements++
			}
		}
	}
	if len(groups) > 100 || requirements < 1 || requirements > 10000 {
		return domain.Invalid("Matrix import requires at most 100 groups and 1..10000 nonblank requirements")
	}
	return nil
}

func importMatrixInput(ctx context.Context, tx *sql.Tx, user string, doc workbook.Document) (MatrixInput, error) {
	in := MatrixInput{Name: doc.Name, Scope: "personal", Visibility: "private"}
	levels := make(map[string]string, len(doc.Levels))
	for i, name := range doc.Levels {
		key := importKey("level", name)
		levels[name] = key
		in.Levels = append(in.Levels, LevelInput{Key: key, Name: name, Order: i})
	}
	groups := make(map[string]string)
	usedSkills := make(map[string]bool)
	for i, row := range doc.Rows {
		group, ok := groups[row.Category]
		if !ok {
			group = importKey("group", row.Category)
			groups[row.Category] = group
			in.Groups = append(in.Groups, GroupInput{Key: group, Name: row.Category, Weight: 1})
		}
		skill, err := importSkill(ctx, tx, user, row.Category, row.Skill, usedSkills)
		if err != nil {
			return MatrixInput{}, err
		}
		usedSkills[skill] = true
		in.Skills = append(in.Skills, MatrixSkillInput{SkillID: skill, GroupKey: group, Weight: 1, Order: i})
		for _, level := range doc.Levels {
			if description := strings.TrimSpace(row.Requirements[level]); description != "" {
				in.Requirements = append(in.Requirements, RequirementInput{SkillID: skill, LevelKey: levels[level], Description: description, Weight: 1})
			}
		}
	}
	return in, nil
}

func importKey(prefix, value string) string {
	var slug strings.Builder
	dash := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			_, _ = slug.WriteRune(r) // strings.Builder writes cannot fail.
			dash = false
		} else if slug.Len() > 0 && !dash {
			_ = slug.WriteByte('-') // strings.Builder writes cannot fail.
			dash = true
		}
		if slug.Len() >= 32 {
			break
		}
	}
	return prefix + "-" + strings.Trim(slug.String(), "-") + "-" + digest(value)[:16]
}

func importSkill(ctx context.Context, tx *sql.Tx, user, category, name string, used map[string]bool) (string, error) {
	candidates, err := jsonRows(ctx, tx, `SELECT jsonb_build_object('id',s.id) FROM skills s WHERE (s.name=$2 OR s.key=$2) AND `+evidenceSkillVisible+` ORDER BY s.id LIMIT 2`, user, name)
	if err != nil {
		return "", err
	}
	if len(candidates) == 1 {
		id, _ := candidates[0]["id"].(string)
		if !used[id] {
			return id, nil
		}
	}
	identity, err := json.Marshal([2]string{category, name})
	if err != nil {
		return "", err
	}
	key := importKey("import", string(identity))
	var categoryExists bool
	if checkErr := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM skill_categories WHERE key='engineering_core')`).Scan(&categoryExists); checkErr != nil {
		return "", checkErr
	}
	if !categoryExists {
		return "", domain.Invalid("engineering_core skill category must be seeded before importing new skills")
	}
	id := ID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO skills(id,key,name,category,scope,owner_id) VALUES($1,$2,$3,'engineering_core','personal',$4) ON CONFLICT(scope,owner_id,key) DO NOTHING`, id, key, name, user); err != nil {
		return "", err
	}
	var existingName, existingCategory string
	var active bool
	if checkErr := tx.QueryRowContext(ctx, `SELECT id,name,category,active FROM skills WHERE scope='personal' AND owner_id=$1 AND key=$2`, user, key).Scan(&id, &existingName, &existingCategory, &active); checkErr != nil {
		return "", checkErr
	}
	if existingName != name || existingCategory != "engineering_core" || !active || used[id] {
		return "", domain.Conflict("Imported skill key conflicts with existing skill")
	}
	return id, nil
}

func exportWorkbook(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	version, userMatrixID := c.Query.Get("matrix_version_id"), c.Query.Get("user_matrix_id")
	if version != "" && userMatrixID != "" {
		return nil, domain.Invalid("Specify only matrix_version_id or user_matrix_id")
	}
	if version == "" {
		if userMatrixID != "" {
			if _, err := uuid.Parse(userMatrixID); err != nil {
				return nil, domain.Invalid("Invalid user matrix ID")
			}
		}
		assigned, err := userMatrix(ctx, tx, c.Actor.UserID, userMatrixID)
		if err != nil {
			return nil, mapError(err)
		}
		version, _ = assigned["matrix_version_id"].(string)
		userMatrixID, _ = assigned["id"].(string)
	} else {
		if _, err := uuid.Parse(version); err != nil {
			return nil, domain.Invalid("Invalid matrix version ID")
		}
		if _, err := accessibleVersion(ctx, tx, version, c.Actor.UserID); err != nil {
			return nil, mapError(err)
		}
	}
	doc, err := exportDocument(ctx, tx, version, userMatrixID)
	if err != nil {
		return nil, err
	}
	if userMatrixID != "" && (strings.EqualFold(strings.TrimSpace(doc.Name), "Assessment") || strings.EqualFold(strings.TrimSpace(doc.Name), "Evidence")) {
		doc.Name = "Matrix"
	}
	data, err := workbook.Export(doc)
	if err != nil {
		return nil, domain.Invalid("Matrix cannot be exported within XLSX limits: " + err.Error())
	}
	if userMatrixID != "" {
		data, err = exportPersonalSheets(ctx, tx, data, c.Actor.UserID, userMatrixID, version)
		if err != nil {
			return nil, err
		}
	}
	return domain.Result{"content_base64": base64.StdEncoding.EncodeToString(data), "filename": "matrix-" + version + ".xlsx", "content_type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"}, nil
}

func exportDocument(ctx context.Context, tx *sql.Tx, version, userMatrixID string) (workbook.Document, error) {
	var doc workbook.Document
	if checkErr := tx.QueryRowContext(ctx, `SELECT m.name FROM matrices m JOIN matrix_versions v ON v.matrix_id=m.id WHERE v.id=$1`, version).Scan(&doc.Name); checkErr != nil {
		return doc, checkErr
	}
	levels, err := jsonRows(ctx, tx, `SELECT jsonb_build_object('id',id,'name',name) FROM levels WHERE matrix_version_id=$1 ORDER BY position,id`, version)
	if err != nil {
		return doc, err
	}
	levelNames := make(map[string]string)
	seenNames := make(map[string]bool)
	for _, level := range levels {
		id, _ := level["id"].(string)
		name, _ := level["name"].(string)
		if seenNames[name] || name == "Category" || name == "Skill" {
			name = name + " [" + id + "]"
		}
		seenNames[name] = true
		levelNames[id] = name
		doc.Levels = append(doc.Levels, name)
	}
	rows, err := jsonRows(ctx, tx, `SELECT jsonb_build_object('id',ms.id,'name',s.name,'skill_id',s.id,'category',g.name)
 FROM matrix_skills ms JOIN skills s ON s.id=ms.skill_id JOIN competency_groups g ON g.id=ms.group_id
 WHERE ms.matrix_version_id=$1 ORDER BY ms.position,ms.id`, version)
	if err != nil {
		return doc, err
	}
	indices := make(map[string]int, len(rows))
	seenRows := make(map[[2]string]bool)
	for _, row := range rows {
		id, _ := row["id"].(string)
		name, _ := row["name"].(string)
		category, _ := row["category"].(string)
		if seenRows[[2]string{category, name}] {
			name = name + " [" + id + "]"
		}
		seenRows[[2]string{category, name}] = true
		indices[id] = len(doc.Rows)
		doc.Rows = append(doc.Rows, workbook.Row{Category: category, Skill: name, Requirements: make(map[string]string)})
	}
	reqs, err := jsonRows(ctx, tx, `SELECT jsonb_build_object('matrix_skill_id',r.matrix_skill_id,'level_id',r.level_id,'description',COALESCE(o.description,r.description)) FROM requirements r LEFT JOIN personal_overrides o ON o.requirement_id=r.id AND o.user_matrix_id=$2::uuid WHERE r.matrix_version_id=$1 ORDER BY r.id`, version, nullable(userMatrixID))
	if err != nil {
		return doc, err
	}
	for _, req := range reqs {
		skill, _ := req["matrix_skill_id"].(string)
		level, _ := req["level_id"].(string)
		description, _ := req["description"].(string)
		doc.Rows[indices[skill]].Requirements[levelNames[level]] = description
	}
	return doc, nil
}

func exportPersonalSheets(ctx context.Context, tx *sql.Tx, data []byte, user, userMatrixID, version string) (result []byte, err error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	assessments, err := jsonRows(ctx, tx, `SELECT jsonb_build_object('assessment_id',a.id,'period',a.period,'type',a.type,'skill',s.name,'requirement_id',ai.requirement_id,'requirement',ai.requirement_snapshot,'score',ai.score,'status',ai.status,'comment',ai.comment,'na_reason',ai.na_reason)
 FROM assessments a JOIN assessment_items ai ON ai.assessment_id=a.id JOIN requirements r ON r.id=ai.requirement_id JOIN matrix_skills ms ON ms.id=r.matrix_skill_id JOIN skills s ON s.id=ms.skill_id
 WHERE a.id=(SELECT id FROM assessments WHERE user_id=$1 AND user_matrix_id=$2 ORDER BY created_at DESC,id DESC LIMIT 1)
 ORDER BY ms.position,r.id LIMIT 2001`, user, userMatrixID)
	if err != nil {
		return nil, err
	}
	evidence, err := jsonRows(ctx, tx, `SELECT jsonb_build_object('id',e.id,'title',e.title,'description',e.description,'fact_type',e.fact_type,'period_from',e.period_from,'period_to',e.period_to,'source',e.source,'source_url',e.source_url,'status',e.status,'skill',s.name,'requirement_id',em.requirement_id,'confidence',em.confidence,'reason',em.reason)
 FROM evidence e JOIN evidence_matches em ON em.evidence_id=e.id JOIN skills s ON s.id=em.skill_id
 WHERE e.user_id=$1 AND EXISTS(SELECT 1 FROM matrix_skills ms WHERE ms.matrix_version_id=$2 AND ms.skill_id=em.skill_id)
 ORDER BY e.id,em.id LIMIT 2001`, user, version)
	if err != nil {
		return nil, err
	}
	for _, sheet := range []struct {
		name    string
		headers []string
		rows    []domain.Result
	}{
		{"Assessment", []string{"assessment_id", "period", "type", "skill", "requirement_id", "requirement", "score", "status", "comment", "na_reason"}, assessments},
		{"Evidence", []string{"id", "title", "description", "fact_type", "period_from", "period_to", "source", "source_url", "status", "skill", "requirement_id", "confidence", "reason"}, evidence},
	} {
		if len(sheet.rows) > 2000 {
			return nil, domain.Invalid("Personal export exceeds 2000 rows per supplemental sheet")
		}
		if checkErr := writeExportSheet(f, sheet.name, sheet.headers, sheet.rows); checkErr != nil {
			return nil, checkErr
		}
	}
	var buf bytes.Buffer
	if checkErr := f.Write(&buf); checkErr != nil {
		return nil, checkErr
	}
	if _, err := workbook.Sheets(bytes.NewReader(buf.Bytes())); err != nil {
		return nil, domain.Invalid("Personal export exceeds workbook limits")
	}
	return buf.Bytes(), nil
}

func writeExportSheet(f *excelize.File, name string, headers []string, rows []domain.Result) error {
	if _, err := f.NewSheet(name); err != nil {
		return err
	}
	for y := 0; y <= len(rows); y++ {
		for x, header := range headers {
			value := header
			if y > 0 {
				value = ""
				if field := rows[y-1][header]; field != nil {
					value = fmt.Sprint(field)
				}
			}
			if len(value) > workbook.MaxCellBytes {
				return domain.Invalid("Supplemental export cell exceeds 32767 bytes")
			}
			cell, err := excelize.CoordinatesToCellName(x+1, y+1)
			if err != nil {
				return err
			}
			if checkErr := f.SetCellStr(name, cell, value); checkErr != nil {
				return checkErr
			}
		}
	}
	return nil
}
