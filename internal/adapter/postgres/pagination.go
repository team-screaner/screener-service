package postgres

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/google/uuid"
	"github.com/team-screaner/screener-service/internal/domain"
)

type cursor struct {
	After   string `json:"after"`
	Binding string `json:"binding"`
}

func page(c domain.Command) (int, string, error) {
	limit := 50
	if s := c.Query.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			return 0, "", domain.Invalid("limit must be 1..100")
		}
		limit = n
	}
	after := "00000000-0000-0000-0000-000000000000"
	if s := c.Query.Get("cursor"); s != "" {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			return 0, "", domain.Invalid("Invalid cursor")
		}
		var v cursor
		if json.Unmarshal(b, &v) != nil || v.Binding != cursorBinding(c) {
			return 0, "", domain.Invalid("Cursor does not match request")
		}
		if _, err := uuid.Parse(v.After); err != nil {
			return 0, "", domain.Invalid("Invalid cursor")
		}
		after = v.After
	}
	return limit, after, nil
}
func cursorBinding(c domain.Command) string {
	q := url.Values{}
	for k, v := range c.Query {
		if k != "cursor" {
			q[k] = v
		}
	}
	return digest(c.Actor.UserID + "\n" + c.Path + "\n" + q.Encode())
}
func paged(c domain.Command, items []domain.Result, limit int) domain.Result {
	next := ""
	if len(items) > limit {
		items = items[:limit]
		id, _ := items[len(items)-1]["id"].(string)
		b, _ := json.Marshal(cursor{After: id, Binding: cursorBinding(c)})
		next = base64.RawURLEncoding.EncodeToString(b)
	}
	return domain.Result{"items": items, "next_cursor": next}
}
