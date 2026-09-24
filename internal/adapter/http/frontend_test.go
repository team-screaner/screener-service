package http_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	transport "github.com/team-screaner/screener-service/internal/adapter/http"
)

func TestFrontendIsServedWithProtectedAPI(t *testing.T) {
	h := transport.New(nil)
	for _, tc := range []struct {
		path, content string
		status        int
	}{
		{"/", "Screener", 200},
		{"/assets/app.js", "", 200},
		{"/assets/styles.css", "", 200},
		{"/assets/missing.js", "", 404},
		{"/api/v1/me", "unauthorized", 401},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRecorder()
			h.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", tc.path, http.NoBody))
			if r.Code != tc.status || !strings.Contains(r.Body.String(), tc.content) {
				t.Fatalf("%s: got %d, expected %d", tc.path, r.Code, tc.status)
			}
		})
	}
}
