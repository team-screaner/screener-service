package postgres

import (
	"context"
	"database/sql"

	"github.com/team-screaner/screener-service/internal/domain"
)

// Recheck mutable authorization before idempotency replay. State checks (published,
// revision etc.) run only for fresh commands, so completed retries remain stable.
func preauthorize(ctx context.Context, tx *sql.Tx, c domain.Command) error {
	switch c.Operation {
	case "setReviewer", "removeReviewer":
		if _, err := userMatrix(ctx, tx, c.Actor.UserID, c.ID); err != nil {
			return mapError(err)
		}
		return mapError(lockAssignment(ctx, tx, c.ID))
	case "createAssessment":
		in, err := decode[assessmentInput](c.Body)
		if err != nil {
			return err
		}
		if in.Type == "manager" {
			if err = lockAssignment(ctx, tx, in.UserMatrixID); err != nil {
				return mapError(err)
			}
			_, err = reviewAssignment(ctx, tx, in.UserMatrixID, c.Actor.UserID)
			return mapError(err)
		}
		if _, err = userMatrix(ctx, tx, c.Actor.UserID, in.UserMatrixID); err != nil {
			return mapError(err)
		}
		return mapError(lockAssignment(ctx, tx, in.UserMatrixID))
	case "createVersion", "getUpstream", "reviewUpstream":
		_, err := matrixAccess(ctx, tx, c.ID, c.Actor.UserID, true)
		return err
	case "publishVersion", "updateVersion":
		var matrix string
		if err := tx.QueryRowContext(ctx, `SELECT matrix_id FROM matrix_versions WHERE id=$1`, c.ID).Scan(&matrix); err != nil {
			return mapError(err)
		}
		_, err := matrixAccess(ctx, tx, matrix, c.Actor.UserID, true)
		return err
	case "createMatrix":
		in, err := decode[MatrixInput](c.Body)
		if err != nil {
			return err
		}
		if in.Scope == "organization" {
			return canWriteOrg(ctx, tx, in.OrganizationID, c.Actor.UserID)
		}
	case "addMember":
		return canWriteOrg(ctx, tx, c.ID, c.Actor.UserID)
	case "createSkill":
		in, err := decode[struct {
			Scope string `json:"scope"`
			Org   string `json:"organization_id"`
		}](c.Body)
		if err != nil {
			return err
		}
		if in.Scope == "organization" {
			return canWriteOrg(ctx, tx, in.Org, c.Actor.UserID)
		}
	case "deactivateSkill":
		var scope, owner string
		if err := tx.QueryRowContext(ctx, `SELECT scope,COALESCE(owner_id::text,'') FROM skills WHERE id=$1`, c.ID).Scan(&scope, &owner); err != nil {
			return mapError(err)
		}
		if scope == "organization" {
			return canWriteOrg(ctx, tx, owner, c.Actor.UserID)
		}
		if scope != "personal" || owner != c.Actor.UserID {
			return domain.Forbidden()
		}
	}
	return nil
}
