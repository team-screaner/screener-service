// Package postgres provides transactional PostgreSQL persistence for Screener.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"

	"github.com/team-screaner/screener-service/internal/domain"
)

func handleFork(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	switch c.Operation {
	case "forkMatrix", "duplicateMatrix":
		source, err := matrixAccess(ctx, tx, c.ID, c.Actor.UserID, false)
		if err != nil {
			return nil, err
		}
		var sourceVersion string
		if queryErr := tx.QueryRowContext(ctx, `SELECT id FROM matrix_versions WHERE matrix_id=$1 AND status='published' ORDER BY number DESC LIMIT 1`, c.ID).Scan(&sourceVersion); queryErr != nil {
			return nil, queryErr
		}
		in, err := decode[struct{ Name string }](c.Body)
		if err != nil {
			return nil, err
		}
		copied, err := forkVersionInput(ctx, tx, sourceVersion, source)
		if err != nil {
			return nil, err
		}
		copied.Name = in.Name
		copied.Scope = "personal"
		copied.Visibility = "private"
		var parent, parentVersion string
		if c.Operation == "forkMatrix" {
			parent = c.ID
			parentVersion = sourceVersion
		}
		return createMatrixInput(ctx, tx, c.Actor.UserID, copied, parent, parentVersion)
	case "getUpstream", "reviewUpstream":
		return reviewFork(ctx, tx, c)
	default:
		return nil, domain.NotFound()
	}
}

func forkVersionInput(ctx context.Context, tx *sql.Tx, version string, matrix domain.Result) (MatrixInput, error) {
	data, err := versionData(ctx, tx, version)
	if err != nil {
		return MatrixInput{}, err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return MatrixInput{}, err
	}
	in, err := decode[MatrixInput](raw)
	if err != nil {
		return MatrixInput{}, err
	}
	in.Name, _ = matrix["name"].(string)
	in.Description, _ = matrix["description"].(string)
	return in, nil
}

func reviewFork(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	matrix, err := matrixAccess(ctx, tx, c.ID, c.Actor.UserID, c.Operation == "reviewUpstream")
	if err != nil {
		return nil, err
	}
	parent, _ := matrix["parent_matrix_id"].(string)
	base, _ := matrix["parent_version_id"].(string)
	if parent == "" || base == "" {
		return nil, domain.Conflict("Matrix is not a fork")
	}
	source, err := matrixAccess(ctx, tx, parent, c.Actor.UserID, false)
	if err != nil {
		return nil, err
	}
	// A writer already holds the fork row lock. Share-lock its upstream to keep
	// publication and snapshot selection consistent while applying a review.
	if c.Operation == "reviewUpstream" {
		var locked string
		if queryErr := tx.QueryRowContext(ctx, `SELECT id FROM matrices WHERE id=$1 FOR SHARE`, parent).Scan(&locked); queryErr != nil {
			return nil, queryErr
		}
	}
	var latest string
	if queryErr := tx.QueryRowContext(ctx, `SELECT id FROM matrix_versions WHERE matrix_id=$1 AND status='published' ORDER BY number DESC LIMIT 1`, parent).Scan(&latest); queryErr != nil {
		return nil, queryErr
	}
	ignored, _ := matrix["ignored_version_id"].(string)
	if c.Operation == "getUpstream" {
		previous, previousErr := forkVersionInput(ctx, tx, base, source)
		if previousErr != nil {
			return nil, previousErr
		}
		next, nextErr := forkVersionInput(ctx, tx, latest, source)
		if nextErr != nil {
			return nil, nextErr
		}
		added, removed, changed := forkDiff(previous, next)
		return domain.Result{"available": latest != base && latest != ignored, "ignored": latest == ignored, "upstream_version_id": latest, "parent_version_id": base, "added_skills": added, "removed_skills": removed, "changed_requirements": changed}, nil
	}
	in, err := decode[struct {
		Action  string
		Version string `json:"upstream_version_id"`
	}](c.Body)
	if err != nil {
		return nil, err
	}
	if in.Action != "apply" && in.Action != "ignore" {
		return nil, domain.Invalid("Upstream action must be apply or ignore")
	}
	if in.Version != latest || latest == base {
		return nil, domain.Conflict("Review requires the latest unapplied published upstream version")
	}
	if in.Action == "ignore" {
		if _, execErr := tx.ExecContext(ctx, `UPDATE matrices SET ignored_version_id=$2 WHERE id=$1`, c.ID, latest); execErr != nil {
			return nil, execErr
		}
		return domain.Result{"id": c.ID, "ignored": true, "upstream_version_id": latest}, nil
	}
	snapshot, err := forkVersionInput(ctx, tx, latest, source)
	if err != nil {
		return nil, err
	}
	// insertVersion allocates independent content IDs. Existing local drafts and
	// published versions remain intact; this is an explicit replacement draft.
	copied, err := insertVersion(ctx, tx, c.ID, c.Actor.UserID, snapshot)
	if err != nil {
		return nil, err
	}
	if _, execErr := tx.ExecContext(ctx, `UPDATE matrices SET parent_version_id=$2,ignored_version_id=NULL WHERE id=$1`, c.ID, latest); execErr != nil {
		return nil, execErr
	}
	return domain.Result{"id": c.ID, "version": copied, "parent_version_id": latest}, nil
}

func forkDiff(before, after MatrixInput) ([]string, []string, []domain.Result) {
	oldSkills, newSkills := make(map[string]bool), make(map[string]bool)
	for _, s := range before.Skills {
		oldSkills[s.SkillID] = true
	}
	for _, s := range after.Skills {
		newSkills[s.SkillID] = true
	}
	added, removed := []string{}, []string{}
	for id := range newSkills {
		if !oldSkills[id] {
			added = append(added, id)
		}
	}
	for id := range oldSkills {
		if !newSkills[id] {
			removed = append(removed, id)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	previous, next := make(map[string]RequirementInput), make(map[string]RequirementInput)
	keys := make(map[string]bool)
	for _, r := range before.Requirements {
		k := r.SkillID + "\x00" + r.LevelKey
		previous[k] = r
		keys[k] = true
	}
	for _, r := range after.Requirements {
		k := r.SkillID + "\x00" + r.LevelKey
		next[k] = r
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	changed := []domain.Result{}
	for _, key := range ordered {
		old, hadOld := previous[key]
		current, hasNew := next[key]
		if hadOld && hasNew && old == current {
			continue
		}
		identity := old
		if hasNew {
			identity = current
		}
		var oldValue, newValue any
		if hadOld {
			oldValue = requirementComparison(old)
		}
		if hasNew {
			newValue = requirementComparison(current)
		}
		changed = append(changed, domain.Result{"skill_id": identity.SkillID, "level_key": identity.LevelKey, "before": oldValue, "after": newValue})
	}
	return added, removed, changed
}
func requirementComparison(r RequirementInput) domain.Result {
	return domain.Result{"description": r.Description, "required": r.Required, "critical": r.Critical, "weight": r.Weight}
}
