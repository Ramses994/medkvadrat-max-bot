package gateway

import (
	"context"
	"regexp"
	"testing"
	"time"
)

func TestSearchByPhone_NetworkErrorHasNoPhone(t *testing.T) {
	c := New("http://127.0.0.1:1", "t")
	c.http.Timeout = 200 * time.Millisecond
	_, err := c.SearchByPhone(context.Background(), "79265906994")
	if err == nil {
		t.Fatal("expected error")
	}
	phone := regexp.MustCompile(`7\d{10}|79265906994`)
	if phone.MatchString(err.Error()) {
		t.Fatalf("phone leaked: %v", err)
	}
}
