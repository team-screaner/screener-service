package postgres

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/team-screaner/screener-service/internal/domain"
)

// Store executes authorized commands atomically against PostgreSQL.
type Store struct {
	db        *sql.DB
	responses cipher.AEAD
}

// New is intended for isolated tests; runtime supplies a persistent response key.
func New(db *sql.DB) *Store {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return NewWithResponseKey(db, key)
}

// ID creates a time-ordered UUIDv7 for a new resource.
func ID() string             { return uuid.Must(uuid.NewV7()).String() }
func digest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func secret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Authenticate resolves an active, unexpired bearer token.
func (s *Store) Authenticate(ctx context.Context, token string) (domain.Actor, error) {
	var a domain.Actor
	var scopes []byte
	err := s.db.QueryRowContext(ctx, `SELECT id,user_id,kind,to_json(scopes) FROM api_tokens WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>now()`, digest(token)).Scan(&a.TokenID, &a.UserID, &a.Kind, &scopes)
	if errors.Is(err, sql.ErrNoRows) {
		return a, domain.Unauthorized()
	}
	if err != nil {
		return a, fmt.Errorf("authenticate: %w", err)
	}
	if decodeErr := json.Unmarshal(scopes, &a.Scopes); decodeErr != nil {
		return a, decodeErr
	}
	return a, nil
}

// Execute applies authorization, idempotency and audit within one transaction.
func (s *Store) Execute(ctx context.Context, c domain.Command) (domain.Result, error) {
	opts := &sql.TxOptions{}
	if c.Method == "GET" {
		opts.Isolation = sql.LevelRepeatableRead
	}
	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if authErr := preauthorize(ctx, tx, c); authErr != nil {
		return nil, authErr
	}
	mutating := c.Method != "GET"
	scope := c.Actor.UserID + ":" + c.Actor.Kind
	op := c.Method + " " + c.Path
	if c.Operation == "register" {
		var in struct {
			Email string `json:"email"`
		}
		if decodeErr := json.Unmarshal(c.Body, &in); decodeErr != nil {
			return nil, domain.Invalid("Invalid JSON")
		}
		scope = "register:" + strings.ToLower(strings.TrimSpace(in.Email))
	}
	keyed := mutating && c.Operation != "login" && c.Operation != "logout"
	if keyed {
		if c.Key == "" || len(c.Key) > 200 {
			return nil, domain.Invalid("Idempotency-Key must have 1..200 characters")
		}
		var value any
		if len(c.Body) > 0 {
			if decodeErr := json.Unmarshal(c.Body, &value); decodeErr != nil {
				return nil, domain.Invalid("Invalid JSON")
			}
		}
		canonical, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			return nil, marshalErr
		}
		hash := digest(string(canonical))
		_, err = tx.ExecContext(ctx, `INSERT INTO idempotency(scope,operation,key,request_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, scope, op, c.Key, hash)
		if err != nil {
			return nil, mapError(err)
		}
		var priorHash string
		var response []byte
		if queryErr := tx.QueryRowContext(ctx, `SELECT request_hash,response FROM idempotency WHERE scope=$1 AND operation=$2 AND key=$3 FOR UPDATE`, scope, op, c.Key).Scan(&priorHash, &response); queryErr != nil {
			return nil, queryErr
		}
		if priorHash != hash {
			return nil, &domain.Error{Status: 422, Code: "idempotency_conflict", Message: "Idempotency-Key was used with a different request"}
		}
		if response != nil {
			var result domain.Result
			plain, openErr := s.openResponse(response, scope+"\n"+op+"\n"+c.Key)
			if openErr != nil {
				return nil, openErr
			}
			if decodeErr := json.Unmarshal(plain, &result); decodeErr != nil {
				return nil, decodeErr
			}
			return result, nil
		}
	}
	out, err := s.dispatch(ctx, tx, c)
	if err != nil {
		return nil, mapError(err)
	}
	if mutating {
		actor := c.Actor.UserID
		if actor == "" {
			actor, _ = out["user_id"].(string)
		}
		var actorID any
		if actor != "" {
			actorID = actor
		}
		resourceID := c.ID
		if resourceID == "" {
			resourceID, _ = out["id"].(string)
		}
		if resourceID == "" {
			resourceID, _ = out["requirement_id"].(string)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO audit_log(id,actor_id,operation,resource_id) VALUES($1,$2,$3,$4)`, ID(), actorID, c.Operation, resourceID); err != nil {
			return nil, err
		}
	}
	if keyed {
		data, marshalErr := json.Marshal(out)
		if marshalErr != nil {
			return nil, marshalErr
		}
		data, err = s.sealResponse(data, scope+"\n"+op+"\n"+c.Key)
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE idempotency SET response=$4 WHERE scope=$1 AND operation=$2 AND key=$3`, scope, op, c.Key, data); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, mapError(err)
	}
	return out, nil
}
func mapError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.NotFound()
	}
	var p *pgconn.PgError
	if errors.As(err, &p) {
		switch p.Code {
		case "23505":
			return domain.Conflict("Resource already exists")
		case "23503", "23514", "22P02", "22007", "22008":
			return domain.Invalid("Invalid resource reference or value")
		case "40001", "40P01":
			return domain.Conflict("Concurrent update; retry the request")
		}
	}
	return err
}
func decode[T any](b []byte) (T, error) {
	var x T
	err := json.Unmarshal(b, &x)
	if err != nil {
		return x, domain.Invalid("Invalid request body")
	}
	return x, nil
}
func jsonRow(ctx context.Context, tx *sql.Tx, q string, args ...any) (domain.Result, error) {
	var b []byte
	if err := tx.QueryRowContext(ctx, q, args...).Scan(&b); err != nil {
		return nil, err
	}
	var out domain.Result
	err := json.Unmarshal(b, &out)
	return out, err
}
func jsonRows(ctx context.Context, tx *sql.Tx, q string, args ...any) (out []domain.Result, resultErr error) {
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	out = []domain.Result{}
	for rows.Next() {
		var b []byte
		var v domain.Result
		if scanErr := rows.Scan(&b); scanErr != nil {
			return nil, scanErr
		}
		if decodeErr := json.Unmarshal(b, &v); decodeErr != nil {
			return nil, decodeErr
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func parseDate(s string) (any, error) {
	if s == "" {
		return nil, domain.Invalid("Date required")
	}
	v, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, domain.Invalid("Invalid date")
	}
	return v, nil
}
