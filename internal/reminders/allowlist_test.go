package reminders

import "testing"

func TestTargetPatients_AllowlistFilter(t *testing.T) {
	linked := []int64{1, 6789, 999}
	allow := []int64{1, 1587578}
	got := TargetPatients(linked, allow)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestTargetPatients_EmptyAllowlistUsesAllLinked(t *testing.T) {
	linked := []int64{1, 2}
	got := TargetPatients(linked, nil)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestTargetPatients_NoLinked(t *testing.T) {
	if got := TargetPatients(nil, []int64{1}); got != nil {
		t.Fatalf("got %v", got)
	}
}
