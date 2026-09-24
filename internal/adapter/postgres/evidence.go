// Package postgres implements transactional persistence for the screening service.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/team-screaner/screener-service/internal/domain"
)

// EvidenceInput describes a fact and its proposed skill matches.
type EvidenceInput struct {
	ExternalID  string               `json:"external_id"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	FactType    string               `json:"fact_type"`
	PeriodFrom  string               `json:"period_from"`
	PeriodTo    string               `json:"period_to"`
	Source      string               `json:"source"`
	SourceURL   string               `json:"source_url"`
	Matches     []EvidenceMatchInput `json:"matches"`
}

// EvidenceMatchInput identifies a visible skill and optional assigned requirement.
type EvidenceMatchInput struct {
	SkillKey      string  `json:"skill_key"`
	RequirementID string  `json:"requirement_id"`
	Confidence    float64 `json:"confidence"`
	Reason        string  `json:"reason"`
}

// EvidenceBatchInput is an atomic import with an optional quarter.
type EvidenceBatchInput struct {
	Period string          `json:"period"`
	Items  []EvidenceInput `json:"items"`
}

// EvidenceReviewInput applies an optimistic owner review.
type EvidenceReviewInput struct {
	Status      string  `json:"status"`
	Version     int     `json:"version"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

// HandleEvidence uses the caller's transaction. Any returned error requires a
// rollback so a failed batch never persists a prefix of its items.
func HandleEvidence(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	switch c.Operation {
	case "createEvidence":
		in, err := decode[EvidenceInput](c.Body)
		if err != nil {
			return nil, err
		}
		return insertEvidence(ctx, tx, c.Actor, in, "", false)
	case "batchEvidence":
		in, err := decode[EvidenceBatchInput](c.Body)
		if err != nil {
			return nil, err
		}
		if len(in.Items) < 1 || len(in.Items) > 100 {
			return nil, domain.Invalid("Batch requires 1..100 items")
		}
		if in.Period != "" {
			if _, _, err := evidenceQuarter(in.Period); err != nil {
				return nil, err
			}
		}
		items := make([]domain.Result, 0, len(in.Items))
		for i := range in.Items {
			result, err := insertEvidence(ctx, tx, c.Actor, in.Items[i], in.Period, true)
			if err != nil {
				return nil, err
			}
			items = append(items, result)
		}
		return domain.Result{"items": items, "next_cursor": ""}, nil
	case "listEvidence":
		limit, after, err := page(c)
		if err != nil {
			return nil, err
		}
		items, err := jsonRows(ctx, tx, `SELECT `+evidenceJSON+` FROM evidence e WHERE e.user_id=$1 AND e.id>$2 ORDER BY e.id LIMIT $3`, c.Actor.UserID, after, limit+1)
		if err != nil {
			return nil, err
		}
		return paged(c, items, limit), nil
	case "reviewEvidence":
		return reviewEvidence(ctx, tx, c)
	default:
		return nil, domain.NotFound()
	}
}

const evidenceJSON = `jsonb_build_object(
 'id',e.id,'user_id',e.user_id,'title',e.title,'description',e.description,
 'fact_type',e.fact_type,'period_from',e.period_from,'period_to',e.period_to,
 'source',e.source,'source_url',e.source_url,'external_id',e.external_id,
 'status',e.status,'version',e.revision,'created_at',e.created_at,'updated_at',e.updated_at,
 'matches',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',em.id,'skill_key',s.key,
 'requirement_id',em.requirement_id,'confidence',em.confidence,'reason',em.reason) ORDER BY em.id)
 FROM evidence_matches em JOIN skills s ON s.id=em.skill_id WHERE em.evidence_id=e.id),'[]'::jsonb))`

func evidenceResult(ctx context.Context, tx *sql.Tx, user, id string) (domain.Result, error) {
	return jsonRow(ctx, tx, `SELECT `+evidenceJSON+` FROM evidence e WHERE e.id=$1 AND e.user_id=$2`, id, user)
}

func insertEvidence(ctx context.Context, tx *sql.Tx, actor domain.Actor, in EvidenceInput, period string, batch bool) (domain.Result, error) {
	if checkErr := validateEvidence(&in, period, batch); checkErr != nil {
		return nil, checkErr
	}
	canonical, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	hash := digest(string(canonical))
	var factExists bool
	if checkErr := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fact_types WHERE key=$1)`, in.FactType).Scan(&factExists); checkErr != nil {
		return nil, checkErr
	}
	if !factExists {
		return nil, domain.Invalid("Unknown fact type")
	}
	status := "suggested"
	if actor.Kind == "session" && in.Source == "manual" && !batch {
		status = "accepted"
	}
	id := ID()
	err = tx.QueryRowContext(ctx, `INSERT INTO evidence(id,user_id,title,description,fact_type,period_from,period_to,source,source_url,external_id,status,created_by,import_hash)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$2,$12)
 ON CONFLICT(user_id,source,external_id) DO NOTHING RETURNING id`, id, actor.UserID, in.Title, in.Description, in.FactType, nullable(in.PeriodFrom), nullable(in.PeriodTo), in.Source, in.SourceURL, nullable(in.ExternalID), status, hash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var original sql.NullString
		if checkErr := tx.QueryRowContext(ctx, `SELECT id,import_hash FROM evidence WHERE user_id=$1 AND source=$2 AND external_id=$3 FOR UPDATE`, actor.UserID, in.Source, in.ExternalID).Scan(&id, &original); checkErr != nil {
			return nil, checkErr
		}
		if !original.Valid || original.String != hash {
			return nil, domain.Conflict("External ID already exists with different imported content")
		}
		return evidenceResult(ctx, tx, actor.UserID, id)
	}
	if err != nil {
		return nil, err
	}
	for _, match := range in.Matches {
		skill, err := evidenceSkill(ctx, tx, actor.UserID, match)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO evidence_matches(id,evidence_id,skill_id,requirement_id,confidence,reason,created_by) VALUES($1,$2,$3,$4,$5,$6,$7)`, ID(), id, skill, nullable(match.RequirementID), match.Confidence, match.Reason, actor.UserID); err != nil {
			return nil, err
		}
	}
	return evidenceResult(ctx, tx, actor.UserID, id)
}

func validateEvidence(in *EvidenceInput, period string, batch bool) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.ExternalID = strings.TrimSpace(in.ExternalID)
	if in.Title == "" || len(in.Title) > 500 || in.Description == "" || len(in.Description) > 50000 {
		return domain.Invalid("Evidence needs a title (max 500 bytes) and description (max 50000 bytes)")
	}
	if batch && in.ExternalID == "" {
		return domain.Invalid("Batch item external_id is required")
	}
	if len(in.ExternalID) > 500 {
		return domain.Invalid("External ID is too long")
	}
	switch in.Source {
	case "manual", "github", "gitlab", "jira", "confluence", "linear", "ai_import", "api", "other":
	default:
		return domain.Invalid("Invalid evidence source")
	}
	if in.SourceURL != "" {
		u, err := url.Parse(in.SourceURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || len(in.SourceURL) > 4096 {
			return domain.Invalid("Source URL must be an absolute HTTP(S) URL without credentials")
		}
	}
	if in.PeriodFrom == "" && in.PeriodTo == "" && period != "" {
		start, end, err := evidenceQuarter(period)
		if err != nil {
			return err
		}
		in.PeriodFrom = start
		in.PeriodTo = end
	}
	if (in.PeriodFrom == "") != (in.PeriodTo == "") {
		return domain.Invalid("Both period_from and period_to are required together")
	}
	if in.PeriodFrom != "" {
		start, err := time.Parse("2006-01-02", in.PeriodFrom)
		if err != nil {
			return domain.Invalid("Invalid period_from date")
		}
		end, err := time.Parse("2006-01-02", in.PeriodTo)
		if err != nil {
			return domain.Invalid("Invalid period_to date")
		}
		if end.Before(start) {
			return domain.Invalid("period_to precedes period_from")
		}
	}
	if len(in.Matches) > 100 {
		return domain.Invalid("At most 100 matches are allowed")
	}
	seen := make(map[[2]string]bool, len(in.Matches))
	for i := range in.Matches {
		m := &in.Matches[i]
		m.SkillKey = strings.TrimSpace(m.SkillKey)
		m.Reason = strings.TrimSpace(m.Reason)
		if m.SkillKey == "" || m.Reason == "" || len(m.Reason) > 10000 || math.IsNaN(m.Confidence) || math.IsInf(m.Confidence, 0) || m.Confidence < 0 || m.Confidence > 1 {
			return domain.Invalid("Match requires skill_key, reason and finite confidence in [0,1]")
		}
		if m.RequirementID != "" {
			if _, err := uuid.Parse(m.RequirementID); err != nil {
				return domain.Invalid("Invalid requirement ID")
			}
		}
		key := [2]string{m.SkillKey, m.RequirementID}
		if seen[key] {
			return domain.Invalid("Duplicate evidence match")
		}
		seen[key] = true
	}
	sort.Slice(in.Matches, func(i, j int) bool {
		a, b := in.Matches[i], in.Matches[j]
		if a.SkillKey == b.SkillKey {
			return a.RequirementID < b.RequirementID
		}
		return a.SkillKey < b.SkillKey
	})
	return nil
}

func evidenceQuarter(period string) (string, string, error) {
	if len(period) != 7 || period[4:6] != "-Q" {
		return "", "", domain.Invalid("Period must use YYYY-Q1..Q4")
	}
	year, err := strconv.Atoi(period[:4])
	if err != nil || year < 1 {
		return "", "", domain.Invalid("Invalid period year")
	}
	quarter, err := strconv.Atoi(period[6:])
	if err != nil || quarter < 1 || quarter > 4 {
		return "", "", domain.Invalid("Invalid period quarter")
	}
	start := time.Date(year, time.Month(1+(quarter-1)*3), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 3, 0).AddDate(0, 0, -1)
	return start.Format("2006-01-02"), end.Format("2006-01-02"), nil
}

const evidenceSkillVisible = `s.active AND (s.scope='system' OR (s.scope='personal' AND s.owner_id=$1) OR (s.scope='organization' AND EXISTS(SELECT 1 FROM organization_members om WHERE om.organization_id=s.owner_id AND om.user_id=$1)))`

func evidenceSkill(ctx context.Context, tx *sql.Tx, user string, match EvidenceMatchInput) (skillID string, err error) {
	if match.RequirementID != "" {
		var skill string
		err = tx.QueryRowContext(ctx, `SELECT s.id FROM requirements r JOIN matrix_skills ms ON ms.id=r.matrix_skill_id JOIN skills s ON s.id=ms.skill_id
 JOIN matrix_versions mv ON mv.id=r.matrix_version_id
 WHERE r.id=$3 AND s.key=$2 AND `+evidenceSkillVisible+` AND mv.status='published' AND ms.active
 AND EXISTS(SELECT 1 FROM user_matrices um WHERE um.user_id=$1 AND um.matrix_version_id=r.matrix_version_id AND um.status='active')`, user, match.SkillKey, match.RequirementID).Scan(&skill)
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.Invalid("Requirement must match the skill in an assigned published matrix")
		}
		return skill, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.id FROM skills s WHERE s.key=$2 AND `+evidenceSkillVisible+` ORDER BY s.id LIMIT 2`, user, match.SkillKey)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var ids []string
	for rows.Next() {
		var id string
		if checkErr := rows.Scan(&id); checkErr != nil {
			return "", checkErr
		}
		ids = append(ids, id)
	}
	if checkErr := rows.Err(); checkErr != nil {
		return "", checkErr
	}
	if len(ids) != 1 {
		return "", domain.Invalid("Skill key must resolve to exactly one visible active skill")
	}
	return ids[0], nil
}

func reviewEvidence(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	if c.Actor.Kind != "session" {
		return nil, domain.Forbidden()
	}
	if _, err := uuid.Parse(c.ID); err != nil {
		return nil, domain.Invalid("Invalid evidence ID")
	}
	in, err := decode[EvidenceReviewInput](c.Body)
	if err != nil {
		return nil, err
	}
	if (in.Status != "accepted" && in.Status != "rejected") || in.Version < 1 {
		return nil, domain.Invalid("Review requires accepted/rejected status and a positive version")
	}
	for _, field := range []struct {
		value *string
		max   int
	}{{in.Title, 500}, {in.Description, 50000}} {
		if field.value != nil {
			*field.value = strings.TrimSpace(*field.value)
			if *field.value == "" || len(*field.value) > field.max {
				return nil, domain.Invalid("Review text cannot be blank or exceed field limits")
			}
		}
	}
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM evidence WHERE id=$1 AND user_id=$2 FOR UPDATE`, c.ID, c.Actor.UserID).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.NotFound()
		}
		return nil, err
	}
	if current != in.Version {
		return nil, domain.Conflict("Evidence version changed")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE evidence SET status=$3,title=COALESCE($4,title),description=COALESCE($5,description),revision=revision+1,updated_at=now() WHERE id=$1 AND user_id=$2`, c.ID, c.Actor.UserID, in.Status, in.Title, in.Description); err != nil {
		return nil, fmt.Errorf("review evidence: %w", err)
	}
	return evidenceResult(ctx, tx, c.Actor.UserID, c.ID)
}
