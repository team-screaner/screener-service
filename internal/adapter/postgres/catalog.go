package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/team-screaner/screener-service/internal/catalog"
)

// Seed adds the versioned system catalog without overwriting existing customization.
// Deterministic UUIDv5 IDs are limited to immutable seed data; user resources use UUIDv7.
func Seed(ctx context.Context, db *sql.DB) error {
	c, err := catalog.Load()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(72893021)`); err != nil {
		return err
	}
	skills := make(map[string]catalog.Skill, len(c.Skills))
	for _, s := range c.Skills {
		skills[s.Key] = s
		if _, err = tx.ExecContext(ctx, `INSERT INTO skill_categories(key,name) VALUES($1,$2) ON CONFLICT DO NOTHING`, s.Category, strings.ReplaceAll(s.Category, "_", " ")); err != nil {
			return fmt.Errorf("seed category: %w", err)
		}
		id := seedID("skill", s.Key)
		if _, err = tx.ExecContext(ctx, `INSERT INTO skills(id,key,name,description,category,scope) VALUES($1,$2,$3,$4,$5,'system') ON CONFLICT DO NOTHING`, id, s.Key, s.Name, s.Description, s.Category); err != nil {
			return fmt.Errorf("seed skill: %w", err)
		}
		for _, signal := range s.Signals {
			if _, err = tx.ExecContext(ctx, `INSERT INTO evidence_signals(skill_id,signal) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, signal); err != nil {
				return fmt.Errorf("seed signal: %w", err)
			}
		}
	}
	for _, f := range c.FactTypes {
		if _, err = tx.ExecContext(ctx, `INSERT INTO fact_types(key,name) VALUES($1,$2) ON CONFLICT DO NOTHING`, f.Key, f.Name); err != nil {
			return fmt.Errorf("seed fact type: %w", err)
		}
	}
	for _, template := range c.Templates {
		if err = seedTemplate(ctx, tx, template, skills); err != nil {
			return fmt.Errorf("seed template %s: %w", template.Key, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}
	return nil
}
func seedID(kind, key string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://github.com/team-screaner/screener-service/seed/v1/"+kind+"/"+key)).String()
}
func seedTemplate(ctx context.Context, tx *sql.Tx, t catalog.Template, skills map[string]catalog.Skill) error {
	matrixID := seedID("matrix", t.Key)
	versionID := seedID("version", t.Key+"/1")
	result, err := tx.ExecContext(ctx, `INSERT INTO matrices(id,name,description,scope,visibility,status) VALUES($1,$2,$3,'system','public','published') ON CONFLICT DO NOTHING`, matrixID, t.Name, "Editable starting template; requirements require organization review.")
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 0 {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO matrix_versions(id,matrix_id,number,status) VALUES($1,$2,1,'draft')`, versionID, matrixID); err != nil {
		return err
	}
	for i, l := range t.Levels {
		if _, err = tx.ExecContext(ctx, `INSERT INTO levels(id,matrix_version_id,key,name,description,position) VALUES($1,$2,$3,$4,$5,$6)`, seedID("level", t.Key+"/"+l.Key), versionID, l.Key, l.Name, l.Description, i); err != nil {
			return err
		}
	}
	groups := map[string]bool{}
	for i, key := range t.SkillKeys {
		s := skills[key]
		groupID := seedID("group", t.Key+"/"+s.Category)
		if !groups[s.Category] {
			if _, err = tx.ExecContext(ctx, `INSERT INTO competency_groups(id,matrix_version_id,key,name,weight) VALUES($1,$2,$3,$4,1)`, groupID, versionID, s.Category, strings.ReplaceAll(s.Category, "_", " ")); err != nil {
				return err
			}
			groups[s.Category] = true
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO matrix_skills(id,matrix_version_id,skill_id,group_id,weight,required,active,position) VALUES($1,$2,$3,$4,1,true,true,$5)`, seedID("matrix-skill", t.Key+"/"+key), versionID, seedID("skill", key), groupID, i); err != nil {
			return err
		}
	}
	for _, r := range t.Requirements {
		if _, err = tx.ExecContext(ctx, `INSERT INTO requirements(id,matrix_version_id,matrix_skill_id,level_id,description,required,critical,weight) VALUES($1,$2,$3,$4,$5,true,$6,1)`, seedID("requirement", t.Key+"/"+r.SkillKey+"/"+r.LevelKey), versionID, seedID("matrix-skill", t.Key+"/"+r.SkillKey), seedID("level", t.Key+"/"+r.LevelKey), r.Description, r.Critical); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE matrix_versions SET status='published' WHERE id=$1`, versionID)
	return err
}
