package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"

	"github.com/team-screaner/screener-service/internal/domain"
)

// All review grant changes and assessment writes lock the same assignment. A
// revoked reviewer cannot submit or replay a response after revocation commits.
func lockAssignment(ctx context.Context, tx *sql.Tx, id string) error {
	var found string
	return tx.QueryRowContext(ctx, `SELECT id FROM user_matrices WHERE id=$1 FOR UPDATE`, id).Scan(&found)
}

func reviewAssignment(ctx context.Context, tx *sql.Tx, id, reviewer string) (domain.Result, error) {
	return jsonRow(ctx, tx, `SELECT to_jsonb(u) FROM user_matrices u JOIN matrix_reviewers r ON r.user_matrix_id=u.id WHERE u.id=$1 AND r.reviewer_id=$2 AND u.user_id<>$2 AND u.status='active'`, id, reviewer)
}

func reviewerData(ctx context.Context, tx *sql.Tx, id, owner string) (domain.Result, error) {
	return jsonRow(ctx, tx, `SELECT jsonb_build_object('user_matrix_id',u.id,'reviewer',CASE WHEN p.id IS NULL THEN NULL ELSE jsonb_build_object('id',p.id,'email',p.email,'name',p.name) END) FROM user_matrices u LEFT JOIN matrix_reviewers r ON r.user_matrix_id=u.id LEFT JOIN users p ON p.id=r.reviewer_id WHERE u.id=$1 AND u.user_id=$2`, id, owner)
}

func handleReviews(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	switch c.Operation {
	case "getReviewer":
		return reviewerData(ctx, tx, c.ID, c.Actor.UserID)
	case "setReviewer", "removeReviewer":
		if _, err := userMatrix(ctx, tx, c.Actor.UserID, c.ID); err != nil {
			return nil, err
		}
		if c.Operation == "removeReviewer" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM matrix_reviewers WHERE user_matrix_id=$1`, c.ID); err != nil {
				return nil, err
			}
		} else {
			in, err := decode[struct {
				Email string `json:"email"`
			}](c.Body)
			if err != nil {
				return nil, err
			}
			var reviewer string
			err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE email=$1`, strings.ToLower(strings.TrimSpace(in.Email))).Scan(&reviewer)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, domain.Invalid("Reviewer must have a registered account")
			}
			if err != nil {
				return nil, err
			}
			if reviewer == c.Actor.UserID {
				return nil, domain.Invalid("Cannot be your own manager reviewer")
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO matrix_reviewers(user_matrix_id,reviewer_id) VALUES($1,$2) ON CONFLICT(user_matrix_id) DO UPDATE SET reviewer_id=excluded.reviewer_id,assigned_at=now()`, c.ID, reviewer); err != nil {
				return nil, err
			}
		}
		return reviewerData(ctx, tx, c.ID, c.Actor.UserID)
	case "listReviews":
		limit, after, err := page(c)
		if err != nil {
			return nil, err
		}
		items, err := jsonRows(ctx, tx, `SELECT jsonb_build_object('id',u.id,'user',jsonb_build_object('id',p.id,'name',p.name,'email',p.email),'matrix_name',m.name,'target_level_name',l.name,'assessment_id',(SELECT a.id FROM assessments a WHERE a.user_matrix_id=u.id AND a.type='self' ORDER BY a.id DESC LIMIT 1)) FROM matrix_reviewers r JOIN user_matrices u ON u.id=r.user_matrix_id JOIN users p ON p.id=u.user_id JOIN matrix_versions v ON v.id=u.matrix_version_id JOIN matrices m ON m.id=v.matrix_id JOIN levels l ON l.id=u.target_level_id WHERE r.reviewer_id=$1 AND u.status='active' AND u.id>$2 ORDER BY u.id LIMIT $3`, c.Actor.UserID, after, limit+1)
		return paged(c, items, limit), err
	case "getReview":
		return reviewContext(ctx, tx, c)
	default:
		return nil, domain.NotFound()
	}
}

func reviewContext(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	u, err := reviewAssignment(ctx, tx, c.ID, c.Actor.UserID)
	if err != nil {
		return nil, err
	}
	owner, ok := u["user_id"].(string)
	if !ok {
		return nil, errors.New("invalid assignment owner")
	}
	subject := c
	subject.Actor.UserID = owner
	subject.Query = url.Values{"user_matrix_id": {c.ID}}
	contextData, err := growthContext(ctx, tx, subject)
	if err != nil {
		return nil, err
	}
	// Only accepted evidence linked to exact requirements in the shared matrix.
	evidence, err := jsonRows(ctx, tx, `SELECT `+evidenceJSON+` FROM evidence e WHERE e.user_id=$1 AND e.status IN ('accepted','manager_confirmed') AND EXISTS(SELECT 1 FROM evidence_matches em JOIN requirements r ON r.id=em.requirement_id WHERE em.evidence_id=e.id AND r.matrix_version_id=$2) ORDER BY e.id`, owner, u["matrix_version_id"])
	if err != nil {
		return nil, err
	}
	contextData["evidence"] = evidence
	delete(contextData, "evidence_limit")
	aid, err := latestSelfAssessment(ctx, tx, c.ID)
	if err != nil {
		return nil, err
	}
	out := domain.Result{"context": contextData, "assessment": nil, "manager_assessment": nil}
	if aid == "" {
		return out, nil
	}
	out["assessment"], err = assessmentData(ctx, tx, aid, owner)
	if err != nil {
		return nil, err
	}
	mid, err := latestManagerAssessment(ctx, tx, aid)
	if err != nil {
		return nil, err
	}
	if mid != "" {
		out["manager_assessment"], err = assessmentData(ctx, tx, mid, owner)
	}
	return out, err
}

func latestSelfAssessment(ctx context.Context, tx *sql.Tx, assignment string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM assessments WHERE user_matrix_id=$1 AND type='self' ORDER BY id DESC LIMIT 1`, assignment).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
func latestManagerAssessment(ctx context.Context, tx *sql.Tx, self string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM assessments WHERE reviewed_assessment_id=$1 AND type='manager' ORDER BY id DESC LIMIT 1`, nullable(self)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func managerAssessmentSubject(ctx context.Context, tx *sql.Tx, c domain.Command, in assessmentInput) (domain.Result, error) {
	u, err := reviewAssignment(ctx, tx, in.UserMatrixID, c.Actor.UserID)
	if err != nil {
		return nil, err
	}
	latest, err := latestSelfAssessment(ctx, tx, in.UserMatrixID)
	if err != nil {
		return nil, err
	}
	if latest == "" || latest != in.ReviewedAssessmentID {
		return nil, domain.Conflict("Self assessment changed; reload before reviewing")
	}
	var period string
	if periodErr := tx.QueryRowContext(ctx, `SELECT period FROM assessments WHERE id=$1`, latest).Scan(&period); periodErr != nil {
		return nil, periodErr
	}
	if period != in.Period {
		return nil, domain.Invalid("Manager and self assessments must have the same period")
	}
	// Complete target coverage avoids interpreting unfilled manager scores as zero.
	required, err := jsonRows(ctx, tx, `SELECT jsonb_build_object('id',r.id) FROM requirements r JOIN matrix_skills s ON s.id=r.matrix_skill_id WHERE r.matrix_version_id=$1 AND r.level_id=$2 AND s.active`, u["matrix_version_id"], u["target_level_id"])
	if err != nil {
		return nil, err
	}
	ids := make(map[string]bool, len(required))
	for _, r := range required {
		rid, ok := r["id"].(string)
		if !ok {
			return nil, errors.New("invalid requirement ID")
		}
		ids[rid] = true
	}
	if len(in.Items) != len(ids) {
		return nil, domain.Invalid("Review every active target requirement")
	}
	for _, item := range in.Items {
		if !ids[item.RequirementID] {
			return nil, domain.Invalid("Review only active target requirements")
		}
	}
	return u, nil
}
