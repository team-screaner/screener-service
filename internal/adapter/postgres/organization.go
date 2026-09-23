package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/team-screaner/screener-service/internal/domain"
)

// HandleCatalog handles organization administration and visible skill dictionaries.
func HandleCatalog(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	if c.Actor.UserID == "" {
		return nil, domain.Unauthorized()
	}
	switch c.Operation {
	case "createOrganization":
		return catalogCreateOrganization(ctx, tx, c)
	case "addMember":
		return catalogAddMember(ctx, tx, c)
	case "createSkill":
		return catalogCreateSkill(ctx, tx, c)
	case "deactivateSkill":
		return catalogDeactivateSkill(ctx, tx, c)
	case "listOrganizations", "listMembers", "listSkills", "listFactTypes":
		return catalogList(ctx, tx, c)
	default:
		return nil, domain.NotFound()
	}
}
func catalogCreateOrganization(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	in, err := decode[struct {
		Name string `json:"name"`
	}](c.Body)
	if err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		return nil, domain.Invalid("Organization name must contain 1..200 bytes")
	}
	id := ID()
	if _, err = tx.ExecContext(ctx, `INSERT INTO organizations(id,name) VALUES($1,$2)`, id, in.Name); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO organization_members(organization_id,user_id,role) VALUES($1,$2,'owner')`, id, c.Actor.UserID); err != nil {
		return nil, err
	}
	return domain.Result{"id": id, "name": in.Name, "role": "owner"}, nil
}
func catalogOrgRole(ctx context.Context, tx *sql.Tx, organization, user string) (string, error) {
	var role string
	err := tx.QueryRowContext(ctx, `SELECT role FROM organization_members WHERE organization_id=$1 AND user_id=$2`, organization, user).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.Forbidden()
	}
	return role, err
}
func catalogAdmin(ctx context.Context, tx *sql.Tx, organization, user string) error {
	role, err := catalogOrgRole(ctx, tx, organization, user)
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return domain.Forbidden()
	}
	return nil
}
func catalogAddMember(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	in, err := decode[struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}](c.Body)
	if err != nil {
		return nil, err
	}
	if _, err = uuid.Parse(in.UserID); err != nil {
		return nil, domain.Invalid("Valid user_id required")
	}
	if in.Role != "admin" && in.Role != "manager" && in.Role != "member" {
		return nil, domain.Invalid("Role must be admin, manager or member")
	}
	// Serialize membership changes before reading authorization, including admin revocation.
	var locked string
	if lockErr := tx.QueryRowContext(ctx, `SELECT id FROM organizations WHERE id=$1 FOR UPDATE`, c.ID).Scan(&locked); lockErr != nil {
		return nil, lockErr
	}
	if roleErr := catalogAdmin(ctx, tx, c.ID, c.Actor.UserID); roleErr != nil {
		return nil, roleErr
	}
	var prior string
	err = tx.QueryRowContext(ctx, `SELECT role FROM organization_members WHERE organization_id=$1 AND user_id=$2`, c.ID, in.UserID).Scan(&prior)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if prior == "owner" {
		return nil, domain.Forbidden()
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO organization_members(organization_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT(organization_id,user_id) DO UPDATE SET role=EXCLUDED.role`, c.ID, in.UserID, in.Role); err != nil {
		return nil, err
	}
	return domain.Result{"organization_id": c.ID, "user_id": in.UserID, "role": in.Role}, nil
}
func catalogCreateSkill(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	in, err := decode[struct {
		Key            string   `json:"key"`
		Name           string   `json:"name"`
		Description    string   `json:"description"`
		Category       string   `json:"category"`
		Scope          string   `json:"scope"`
		OrganizationID string   `json:"organization_id"`
		Signals        []string `json:"signals"`
	}](c.Body)
	if err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if strings.TrimSpace(in.Key) == "" || strings.TrimSpace(in.Key) != in.Key || len(in.Key) > 200 || in.Name == "" || len(in.Name) > 200 || len(in.Description) > 20000 || len(in.Signals) > 100 {
		return nil, domain.Invalid("Valid key, name and bounded description/signals required")
	}
	if in.Scope == "" {
		in.Scope = "personal"
	}
	if in.Category == "" {
		in.Category = "engineering_core"
	}
	owner := c.Actor.UserID
	switch in.Scope {
	case "organization":
		if _, err = uuid.Parse(in.OrganizationID); err != nil {
			return nil, domain.Invalid("organization_id required")
		}
		if roleErr := catalogAdmin(ctx, tx, in.OrganizationID, c.Actor.UserID); roleErr != nil {
			return nil, roleErr
		}
		owner = in.OrganizationID
	case "personal":
		if in.OrganizationID != "" {
			return nil, domain.Invalid("Personal skills cannot specify organization_id")
		}
	default:
		return nil, domain.Invalid("Scope must be personal or organization")
	}
	id := ID()
	if _, err = tx.ExecContext(ctx, `INSERT INTO skills(id,key,name,description,category,scope,owner_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, in.Key, in.Name, in.Description, in.Category, in.Scope, owner); err != nil {
		return nil, err
	}
	for _, signal := range in.Signals {
		signal = strings.TrimSpace(signal)
		if signal == "" || len(signal) > 2000 {
			return nil, domain.Invalid("Signals must contain 1..2000 bytes")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO evidence_signals(skill_id,signal) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, signal); err != nil {
			return nil, err
		}
	}
	return jsonRow(ctx, tx, `SELECT to_jsonb(s)||jsonb_build_object('signals',COALESCE((SELECT jsonb_agg(signal ORDER BY signal) FROM evidence_signals WHERE skill_id=s.id),'[]'::jsonb)) FROM skills s WHERE id=$1`, id)
}
func catalogDeactivateSkill(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(c.Body, &body); err != nil {
		return nil, domain.Invalid("Invalid request body")
	}
	var active bool
	raw, ok := body["active"]
	if !ok || len(body) != 1 || string(raw) != "false" || json.Unmarshal(raw, &active) != nil || active {
		return nil, domain.Invalid("Only active=false may be changed; skill keys are immutable")
	}
	var scope string
	var owner sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT scope,owner_id FROM skills WHERE id=$1 FOR UPDATE`, c.ID).Scan(&scope, &owner); err != nil {
		return nil, err
	}
	switch scope {
	case "personal":
		if owner.String != c.Actor.UserID {
			return nil, domain.Forbidden()
		}
	case "organization":
		if err := catalogAdmin(ctx, tx, owner.String, c.Actor.UserID); err != nil {
			return nil, err
		}
	default:
		return nil, domain.Forbidden()
	}
	return jsonRow(ctx, tx, `UPDATE skills SET active=false,updated_at=now() WHERE id=$1 RETURNING to_jsonb(skills)`, c.ID)
}

type catalogCursor struct {
	After string `json:"after"`
	Scope string `json:"scope"`
}

func catalogPagination(c domain.Command) (int, string, string, error) {
	limit := 50
	if raw := c.Query.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			return 0, "", "", domain.Invalid("limit must be 1..100")
		}
		limit = n
	}
	filters := c.Query.Clone()
	filters.Del("cursor")
	filters.Del("limit")
	scope := digest(c.Actor.UserID + "\n" + c.Operation + "\n" + c.ID + "\n" + filters.Encode())
	if token := c.Query.Get("cursor"); token != "" {
		if len(token) > 2048 {
			return 0, "", "", domain.Invalid("Invalid cursor")
		}
		data, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			return 0, "", "", domain.Invalid("Invalid cursor")
		}
		var cursor catalogCursor
		if json.Unmarshal(data, &cursor) != nil || cursor.Scope != scope || cursor.After == "" {
			return 0, "", "", domain.Invalid("Cursor does not match request")
		}
		if c.Operation != "listFactTypes" {
			if _, err := uuid.Parse(cursor.After); err != nil {
				return 0, "", "", domain.Invalid("Invalid cursor")
			}
		}
		return limit, cursor.After, scope, nil
	}
	return limit, "", scope, nil
}
func catalogList(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	limit, after, scope, err := catalogPagination(c)
	if err != nil {
		return nil, err
	}
	var items []domain.Result
	key := "id"
	switch c.Operation {
	case "listOrganizations":
		items, err = jsonRows(ctx, tx, `SELECT to_jsonb(o)||jsonb_build_object('role',m.role) FROM organizations o JOIN organization_members m ON m.organization_id=o.id WHERE m.user_id=$1 AND ($2::uuid IS NULL OR o.id>$2::uuid) ORDER BY o.id LIMIT $3`, c.Actor.UserID, nullable(after), limit+1)
	case "listMembers":
		if _, err = catalogOrgRole(ctx, tx, c.ID, c.Actor.UserID); err != nil {
			return nil, err
		}
		key = "user_id"
		items, err = jsonRows(ctx, tx, `SELECT jsonb_build_object('user_id',m.user_id,'name',u.name,'role',m.role) FROM organization_members m JOIN users u ON u.id=m.user_id WHERE m.organization_id=$1 AND ($2::uuid IS NULL OR m.user_id>$2::uuid) ORDER BY m.user_id LIMIT $3`, c.ID, nullable(after), limit+1)
	case "listFactTypes":
		key = "key"
		items, err = jsonRows(ctx, tx, `SELECT to_jsonb(f) FROM fact_types f WHERE key>$1 ORDER BY key LIMIT $2`, after, limit+1)
	case "listSkills":
		active := c.Query.Get("active")
		if active != "" && active != "true" && active != "false" {
			return nil, domain.Invalid("active must be true or false")
		}
		items, err = jsonRows(ctx, tx, `SELECT to_jsonb(s)||jsonb_build_object('signals',COALESCE((SELECT jsonb_agg(signal ORDER BY signal) FROM evidence_signals WHERE skill_id=s.id),'[]'::jsonb)) FROM skills s WHERE (s.scope='system' OR (s.scope='personal' AND s.owner_id=$1) OR (s.scope='organization' AND EXISTS(SELECT 1 FROM organization_members m WHERE m.organization_id=s.owner_id AND m.user_id=$1))) AND ($2::uuid IS NULL OR s.id>$2::uuid) AND ($3='' OR s.category=$3) AND ($4='' OR s.scope=$4) AND ($5::boolean IS NULL OR s.active=$5::boolean) AND ($6::uuid IS NULL OR (s.scope='organization' AND s.owner_id=$6::uuid)) ORDER BY s.id LIMIT $7`, c.Actor.UserID, nullable(after), c.Query.Get("category"), c.Query.Get("scope"), nullable(active), nullable(c.Query.Get("organization_id")), limit+1)
	}
	if err != nil {
		return nil, err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last, _ := items[len(items)-1][key].(string)
		data, err := json.Marshal(catalogCursor{After: last, Scope: scope})
		if err != nil {
			return nil, err
		}
		next = base64.RawURLEncoding.EncodeToString(data)
	}
	return domain.Result{"items": items, "next_cursor": next}, nil
}
