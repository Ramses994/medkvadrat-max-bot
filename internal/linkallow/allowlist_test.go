package linkallow

import "testing"

func TestSet_EmptyDeniesAll(t *testing.T) {
	s := New(nil)
	if s.Allowed(1) {
		t.Fatal("empty allowlist must deny")
	}
}

func TestSet_Allowed(t *testing.T) {
	s := New([]int64{100, 200})
	if !s.Allowed(100) {
		t.Fatal("100 should be allowed")
	}
	if s.Allowed(999) {
		t.Fatal("999 should be denied")
	}
}
