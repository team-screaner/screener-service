package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/team-screaner/screener-service/internal/domain"
)

// MatrixInput is a complete editable matrix document.
type MatrixInput struct {
	Name           string             `json:"name"`
	Description    string             `json:"description"`
	Scope          string             `json:"scope"`
	OrganizationID string             `json:"organization_id"`
	Visibility     string             `json:"visibility"`
	Levels         []LevelInput       `json:"levels"`
	Groups         []GroupInput       `json:"groups"`
	Skills         []MatrixSkillInput `json:"skills"`
	Requirements   []RequirementInput `json:"requirements"`
}

// LevelInput defines one configurable level, ordered by array position.
type LevelInput struct {
	Key, Name, Description string
	Order                  int `json:"order"`
}

// GroupInput defines a weighted competency group.
type GroupInput struct {
	Key, Name string
	Weight    float64
}

// MatrixSkillInput associates an accessible dictionary skill with a group.
type MatrixSkillInput struct {
	SkillID  string `json:"skill_id"`
	GroupKey string `json:"group_key"`
	Weight   float64
	Required bool
	Active   *bool
	Order    int
}

// RequirementInput defines an expectation for one skill and level.
type RequirementInput struct {
	SkillID            string `json:"skill_id"`
	LevelKey           string `json:"level_key"`
	Description        string
	Required, Critical bool
	Weight             float64
}

func validWeight(v float64) bool { return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }
func canWriteOrg(ctx context.Context, tx *sql.Tx, org, user string) error {
	var role string
	err := tx.QueryRowContext(ctx, `SELECT role FROM organization_members WHERE organization_id=$1 AND user_id=$2`, org, user).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Forbidden()
	}
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return domain.Forbidden()
	}
	return nil
}
func matrixAccess(ctx context.Context, tx *sql.Tx, id, user string, write bool) (domain.Result, error) {
	q := `SELECT to_jsonb(m) FROM matrices m WHERE id=$1`
	if write {
		q += ` FOR UPDATE`
	}
	m, err := jsonRow(ctx, tx, q, id)
	if err != nil {
		return nil, err
	}
	scope, _ := m["scope"].(string)
	owner, _ := m["owner_id"].(string)
	if scope == "personal" && owner == user {
		return m, nil
	}
	if scope == "organization" {
		var role string
		err := tx.QueryRowContext(ctx, `SELECT role FROM organization_members WHERE organization_id=$1 AND user_id=$2`, owner, user).Scan(&role)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil && (!write || role == "owner" || role == "admin") {
			return m, nil
		}
	}
	if !write && (scope == "system" || m["visibility"] == "public" && m["status"] == "published") {
		return m, nil
	}
	return nil, domain.NotFound()
}
func skillAccess(ctx context.Context, tx *sql.Tx, id, user string) error {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM skills s WHERE s.id=$1 AND s.active AND (s.scope='system' OR s.scope='personal' AND s.owner_id=$2 OR s.scope='organization' AND EXISTS(SELECT 1 FROM organization_members m WHERE m.organization_id=s.owner_id AND m.user_id=$2)))`, id, user).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return domain.Invalid("Skill is not active or accessible")
	}
	return nil
}
func createMatrix(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	in, err := decode[MatrixInput](c.Body)
	if err != nil {
		return nil, err
	}
	return createMatrixInput(ctx, tx, c.Actor.UserID, in, "", "")
}
func createMatrixInput(ctx context.Context, tx *sql.Tx, user string, in MatrixInput, parent, parentVersion string) (domain.Result, error) {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 {
		return nil, domain.Invalid("Matrix name required (max 200)")
	}
	if in.Scope == "" {
		in.Scope = "personal"
	}
	if in.Visibility == "" {
		in.Visibility = "private"
	}
	owner := user
	switch in.Scope {
	case "personal":
		if in.Visibility == "organization" {
			return nil, domain.Invalid("Personal matrix cannot have organization visibility")
		}
	case "organization":
		if err := canWriteOrg(ctx, tx, in.OrganizationID, user); err != nil {
			return nil, err
		}
		owner = in.OrganizationID
	default:
		return nil, domain.Invalid("Invalid scope")
	}
	if in.Visibility != "private" && in.Visibility != "organization" && in.Visibility != "public" {
		return nil, domain.Invalid("Invalid visibility")
	}
	id := ID()
	_, err := tx.ExecContext(ctx, `INSERT INTO matrices(id,name,description,scope,owner_id,visibility,parent_matrix_id,parent_version_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, in.Name, in.Description, in.Scope, owner, in.Visibility, nullable(parent), nullable(parentVersion))
	if err != nil {
		return nil, err
	}
	v, err := insertVersion(ctx, tx, id, user, in)
	if err != nil {
		return nil, err
	}
	return domain.Result{"id": id, "name": in.Name, "version": v}, nil
}
func validateMatrix(in MatrixInput) error {
	if len(in.Levels) < 1 || len(in.Levels) > 20 || len(in.Groups) < 1 || len(in.Groups) > 100 || len(in.Skills) < 1 || len(in.Skills) > 500 || len(in.Requirements) < 1 || len(in.Requirements) > 10000 {
		return domain.Invalid("Matrix requires 1..20 levels, 1..100 groups, 1..500 skills, 1..10000 requirements")
	}
	levels, groups, skills, requirements := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, v := range in.Levels {
		if strings.TrimSpace(v.Key) == "" || strings.TrimSpace(v.Name) == "" || levels[v.Key] {
			return domain.Invalid("Level keys and names must be nonempty and unique")
		}
		levels[v.Key] = true
	}
	for _, v := range in.Groups {
		if strings.TrimSpace(v.Key) == "" || strings.TrimSpace(v.Name) == "" || groups[v.Key] || !validWeight(v.Weight) {
			return domain.Invalid("Invalid group or weight")
		}
		groups[v.Key] = true
	}
	for _, v := range in.Skills {
		if !groups[v.GroupKey] || skills[v.SkillID] || !validWeight(v.Weight) {
			return domain.Invalid("Invalid matrix skill")
		}
		skills[v.SkillID] = true
	}
	for _, v := range in.Requirements {
		k := v.SkillID + ":" + v.LevelKey
		if !skills[v.SkillID] || !levels[v.LevelKey] || requirements[k] || !validWeight(v.Weight) || strings.TrimSpace(v.Description) == "" || len(v.Description) > 10000 {
			return domain.Invalid("Invalid or duplicate requirement")
		}
		requirements[k] = true
	}
	return nil
}
func insertVersion(ctx context.Context, tx *sql.Tx, matrix, user string, in MatrixInput) (domain.Result, error) {
	if err := validateMatrix(in); err != nil {
		return nil, err
	}
	for _, v := range in.Skills {
		if err := skillAccess(ctx, tx, v.SkillID, user); err != nil {
			return nil, err
		}
	}
	id := ID()
	_, err := tx.ExecContext(ctx, `INSERT INTO matrix_versions(id,matrix_id,number,status) SELECT $1,$2,COALESCE(max(number),0)+1,'draft' FROM matrix_versions WHERE matrix_id=$2`, id, matrix)
	if err != nil {
		return nil, err
	}
	if checkErr := writeContent(ctx, tx, id, in); checkErr != nil {
		return nil, checkErr
	}
	return versionData(ctx, tx, id)
}
func writeContent(ctx context.Context, tx *sql.Tx, version string, in MatrixInput) error {
	levels, groups, skills := map[string]string{}, map[string]string{}, map[string]string{}
	for i, v := range in.Levels {
		id := ID()
		levels[v.Key] = id
		if _, err := tx.ExecContext(ctx, `INSERT INTO levels(id,matrix_version_id,key,name,description,position) VALUES($1,$2,$3,$4,$5,$6)`, id, version, v.Key, v.Name, v.Description, i); err != nil {
			return err
		}
	}
	for _, v := range in.Groups {
		id := ID()
		groups[v.Key] = id
		if _, err := tx.ExecContext(ctx, `INSERT INTO competency_groups(id,matrix_version_id,key,name,weight) VALUES($1,$2,$3,$4,$5)`, id, version, v.Key, v.Name, v.Weight); err != nil {
			return err
		}
	}
	for i, v := range in.Skills {
		id := ID()
		skills[v.SkillID] = id
		active := v.Active == nil || *v.Active
		if _, err := tx.ExecContext(ctx, `INSERT INTO matrix_skills(id,matrix_version_id,skill_id,group_id,weight,required,active,position) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, version, v.SkillID, groups[v.GroupKey], v.Weight, v.Required, active, i); err != nil {
			return err
		}
	}
	for _, v := range in.Requirements {
		if _, err := tx.ExecContext(ctx, `INSERT INTO requirements(id,matrix_version_id,matrix_skill_id,level_id,description,required,critical,weight) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, ID(), version, skills[v.SkillID], levels[v.LevelKey], v.Description, v.Required, v.Critical, v.Weight); err != nil {
			return err
		}
	}
	return nil
}
func versionData(ctx context.Context, tx *sql.Tx, id string) (domain.Result, error) {
	v, err := jsonRow(ctx, tx, `SELECT to_jsonb(v)||jsonb_build_object('version',revision) FROM matrix_versions v WHERE id=$1`, id)
	if err != nil {
		return nil, err
	}
	for _, x := range []struct{ key, q string }{{"levels", `SELECT to_jsonb(l)||jsonb_build_object('order',position) FROM levels l WHERE matrix_version_id=$1 ORDER BY position,id`}, {"groups", `SELECT to_jsonb(g) FROM competency_groups g WHERE matrix_version_id=$1 ORDER BY id`}, {"skills", `SELECT to_jsonb(m)||jsonb_build_object('skill_key',s.key,'name',s.name,'group_key',g.key,'order',m.position,'signals',COALESCE((SELECT jsonb_agg(signal ORDER BY signal) FROM evidence_signals WHERE skill_id=s.id),'[]'::jsonb)) FROM matrix_skills m JOIN skills s ON s.id=m.skill_id JOIN competency_groups g ON g.id=m.group_id WHERE m.matrix_version_id=$1 ORDER BY m.position,m.id`}, {"requirements", `SELECT to_jsonb(r)||jsonb_build_object('skill_id',m.skill_id,'level_key',l.key,'group_id',m.group_id) FROM requirements r JOIN matrix_skills m ON m.id=r.matrix_skill_id JOIN levels l ON l.id=r.level_id WHERE r.matrix_version_id=$1 ORDER BY l.position,m.position,r.id`}} {
		items, err := jsonRows(ctx, tx, x.q, id)
		if err != nil {
			return nil, err
		}
		v[x.key] = items
	}
	return v, nil
}
func accessibleVersion(ctx context.Context, tx *sql.Tx, id, user string) (domain.Result, error) {
	var mid, status string
	if err := tx.QueryRowContext(ctx, `SELECT matrix_id,status FROM matrix_versions WHERE id=$1`, id).Scan(&mid, &status); err != nil {
		return nil, err
	}
	if _, err := matrixAccess(ctx, tx, mid, user, false); err != nil {
		return nil, err
	}
	if status != "published" {
		if _, err := matrixAccess(ctx, tx, mid, user, true); err != nil {
			return nil, err
		}
	}
	return versionData(ctx, tx, id)
}
func handleMatrix(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	switch c.Operation {
	case "createMatrix":
		return createMatrix(ctx, tx, c)
	case "listMatrices":
		limit, after, err := page(c)
		if err != nil {
			return nil, err
		}
		items, err := jsonRows(ctx, tx, `SELECT to_jsonb(m) FROM matrices m WHERE m.id>$2 AND (scope='system' OR visibility='public' AND status='published' OR scope='personal' AND owner_id=$1 OR scope='organization' AND EXISTS(SELECT 1 FROM organization_members om WHERE om.organization_id=m.owner_id AND om.user_id=$1)) ORDER BY m.id LIMIT $3`, c.Actor.UserID, after, limit+1)
		return paged(c, items, limit), err
	case "getMatrix", "listVersions":
		m, err := matrixAccess(ctx, tx, c.ID, c.Actor.UserID, false)
		if err != nil {
			return nil, err
		}
		_, writeErr := matrixAccess(ctx, tx, c.ID, c.Actor.UserID, true)
		items, err := jsonRows(ctx, tx, `SELECT to_jsonb(v)||jsonb_build_object('version',revision) FROM matrix_versions v WHERE matrix_id=$1 AND (status='published' OR $2) ORDER BY number`, c.ID, writeErr == nil)
		if err != nil {
			return nil, err
		}
		if c.Operation == "listVersions" {
			return domain.Result{"items": items, "next_cursor": ""}, nil
		}
		m["versions"] = items
		return m, nil
	case "getVersion":
		return accessibleVersion(ctx, tx, c.ID, c.Actor.UserID)
	case "createVersion":
		if _, err := matrixAccess(ctx, tx, c.ID, c.Actor.UserID, true); err != nil {
			return nil, err
		}
		in, err := decode[MatrixInput](c.Body)
		if err != nil {
			return nil, err
		}
		return insertVersion(ctx, tx, c.ID, c.Actor.UserID, in)
	case "publishVersion", "updateVersion":
		var mid, status string
		var revision int
		if err := tx.QueryRowContext(ctx, `SELECT matrix_id,status,revision FROM matrix_versions WHERE id=$1 FOR UPDATE`, c.ID).Scan(&mid, &status, &revision); err != nil {
			return nil, err
		}
		if _, err := matrixAccess(ctx, tx, mid, c.Actor.UserID, true); err != nil {
			return nil, err
		}
		if status == "published" {
			return nil, domain.Conflict("Published versions are immutable; create a new draft")
		}
		if c.Operation == "publishVersion" {
			if _, err := tx.ExecContext(ctx, `UPDATE matrix_versions SET status='published',revision=revision+1 WHERE id=$1`, c.ID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE matrices SET status='published' WHERE id=$1`, mid); err != nil {
				return nil, err
			}
			return versionData(ctx, tx, c.ID)
		}
		in, err := decode[struct {
			Expected int         `json:"expected_version"`
			Matrix   MatrixInput `json:"matrix"`
		}](c.Body)
		if err != nil {
			return nil, err
		}
		if in.Expected != revision {
			return nil, domain.Conflict("Stale matrix version")
		}
		if checkErr := validateMatrix(in.Matrix); checkErr != nil {
			return nil, checkErr
		}
		for _, sk := range in.Matrix.Skills {
			if checkErr := skillAccess(ctx, tx, sk.SkillID, c.Actor.UserID); checkErr != nil {
				return nil, checkErr
			}
		}
		for _, table := range []string{"requirements", "matrix_skills", "competency_groups", "levels"} {
			if _, err = tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE matrix_version_id=$1", table), c.ID); err != nil {
				return nil, err
			}
		}
		if checkErr := writeContent(ctx, tx, c.ID, in.Matrix); checkErr != nil {
			return nil, checkErr
		}
		if _, err = tx.ExecContext(ctx, `UPDATE matrix_versions SET revision=revision+1 WHERE id=$1`, c.ID); err != nil {
			return nil, err
		}
		return versionData(ctx, tx, c.ID)
	default:
		return nil, domain.NotFound()
	}
}
