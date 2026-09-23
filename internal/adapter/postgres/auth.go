package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/team-screaner/screener-service/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

func (s *Store) dispatch(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	switch c.Operation {
	case "register", "login":
		return credentials(ctx, tx, c)
	case "getMe":
		return jsonRow(ctx, tx, `SELECT jsonb_build_object('id',id,'email',email,'name',name,'created_at',created_at) FROM users WHERE id=$1`, c.Actor.UserID)
	case "createToken":
		return createToken(ctx, tx, c)
	case "listTokens":
		limit, after, err := page(c)
		if err != nil {
			return nil, err
		}
		items, err := jsonRows(ctx, tx, `SELECT jsonb_build_object('id',id,'name',name,'scopes',scopes,'expires_at',expires_at,'revoked_at',revoked_at) FROM api_tokens WHERE user_id=$1 AND kind='agent' AND id>$2 ORDER BY id LIMIT $3`, c.Actor.UserID, after, limit+1)
		return paged(c, items, limit), err
	case "logout", "revokeToken":
		id := c.ID
		if c.Operation == "logout" {
			id = c.Actor.TokenID
		}
		r, err := tx.ExecContext(ctx, `UPDATE api_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1 AND user_id=$2`, id, c.Actor.UserID)
		if err != nil {
			return nil, err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, domain.NotFound()
		}
		return domain.Result{"revoked": true}, nil
	case "createMatrix", "listMatrices", "getMatrix", "listVersions", "getVersion", "createVersion", "publishVersion", "updateVersion":
		return handleMatrix(ctx, tx, c)
	case "createUserMatrix", "listUserMatrices", "setOverride", "createAssessment", "getAssessment", "listAssessments", "getGrowthContext", "getGaps", "getRadar", "createGrowthPlan", "updateGrowthPlan", "listGrowthPlans":
		return handleGrowth(ctx, tx, c)
	case "createOrganization", "listOrganizations", "addMember", "listMembers", "createSkill", "listSkills", "deactivateSkill", "listFactTypes":
		return HandleCatalog(ctx, tx, c)
	case "createEvidence", "batchEvidence", "listEvidence", "reviewEvidence":
		return HandleEvidence(ctx, tx, c)
	case "forkMatrix", "duplicateMatrix", "getUpstream", "reviewUpstream":
		return handleFork(ctx, tx, c)
	case "importXLSX", "exportXLSX":
		return HandleImportExport(ctx, tx, c)
	case "listAudit":
		return listAudit(ctx, tx, c)
	default:
		return nil, domain.NotFound()
	}
}
func credentials(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	in, err := decode[struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}](c.Body)
	if err != nil {
		return nil, err
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	addr, err := mail.ParseAddress(in.Email)
	if err != nil || addr.Address != in.Email || len(in.Email) > 254 || len(in.Password) < 12 || len(in.Password) > 72 {
		return nil, domain.Invalid("Valid email and password of 12..72 bytes required")
	}
	var id, hash string
	if c.Operation == "register" {
		if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 {
			return nil, domain.Invalid("Name required (max 200 characters)")
		}
		b, hashErr := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			return nil, hashErr
		}
		id = ID()
		if _, err = tx.ExecContext(ctx, `INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,$3,$4)`, id, in.Email, in.Name, string(b)); err != nil {
			return nil, err
		}
	} else {
		err = tx.QueryRowContext(ctx, `SELECT id,password_hash FROM users WHERE email=$1`, in.Email).Scan(&id, &hash)
		if errors.Is(err, sql.ErrNoRows) {
			_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"), []byte(in.Password))
			return nil, domain.Unauthorized()
		}
		if err != nil {
			return nil, err
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
			return nil, domain.Unauthorized()
		}
	}
	return issueToken(ctx, tx, id, "session", "Session", []string{}, 24*time.Hour)
}
func issueToken(ctx context.Context, tx *sql.Tx, userID, kind, name string, scopes []string, ttl time.Duration) (domain.Result, error) {
	token, err := secret()
	if err != nil {
		return nil, err
	}
	id := ID()
	expiry := time.Now().UTC().Add(ttl)
	_, err = tx.ExecContext(ctx, `INSERT INTO api_tokens(id,user_id,name,token_hash,scopes,kind,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, userID, name, digest(token), scopes, kind, expiry)
	return domain.Result{"id": id, "user_id": userID, "token": token, "expires_at": expiry, "scopes": scopes}, err
}
func createToken(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	in, err := decode[struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
		Days   int      `json:"expires_in_days"`
	}](c.Body)
	if err != nil {
		return nil, err
	}
	if in.Days == 0 {
		in.Days = 30
	}
	if in.Days < 1 || in.Days > 90 || strings.TrimSpace(in.Name) == "" || len(in.Scopes) == 0 || len(in.Scopes) > 4 {
		return nil, domain.Invalid("Name, 1..4 scopes and expiry 1..90 days required")
	}
	for _, scope := range in.Scopes {
		if !slices.Contains([]string{"matrix:read", "evidence:read", "evidence:write", "assessment:read"}, scope) {
			return nil, domain.Invalid("Unsupported token scope")
		}
	}
	return issueToken(ctx, tx, c.Actor.UserID, "agent", in.Name, in.Scopes, time.Duration(in.Days)*24*time.Hour)
}
