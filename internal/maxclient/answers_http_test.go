package maxclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnswerCallback_HTTPCases(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"ok", 200, `{"success":true}`, ""},
		{"success false", 200, `{"success":false,"message":"rate"}`, "success=false"},
		{"429", 429, `{"ok":false}`, "MAX API 429"},
		{"500", 500, `oops`, "MAX API 500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := &Client{token: "t", http: srv.Client()}
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/answers", strings.NewReader("{}"))
			err := c.do(req, nil)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
}
