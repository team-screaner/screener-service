package postgres

import (
	"context"
	"database/sql"

	"github.com/team-screaner/screener-service/internal/domain"
)

func listAudit(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	limit, after, err := page(c)
	if err != nil {
		return nil, err
	}
	items, err := jsonRows(ctx, tx, `SELECT to_jsonb(a) FROM audit_log a WHERE actor_id=$1 AND id>$2 ORDER BY id LIMIT $3`, c.Actor.UserID, after, limit+1)
	return paged(c, items, limit), err
}
