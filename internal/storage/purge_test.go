package storage

import (
	"path/filepath"
	"testing"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/linkallow"
)

func TestPurgeLinksOutsideAllowlist(t *testing.T) {
	dir := t.TempDir()
	st, err := New(filepath.Join(dir, "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.Link(1, 100, "79991111111", "Allowed"); err != nil {
		t.Fatal(err)
	}
	if err := st.Link(2, 999, "79992222222", "Blocked"); err != nil {
		t.Fatal(err)
	}
	if err := st.Link(3, 0, "79993333333", "pending_selection"); err != nil {
		t.Fatal(err)
	}

	n, err := st.PurgeLinksOutsideAllowlist(linkallow.New([]int64{100}))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("deleted=%d want 2", n)
	}
	link, err := st.GetByUserID(1)
	if err != nil || link == nil || link.PatientID != 100 {
		t.Fatalf("allowed link: %+v err=%v", link, err)
	}
	if link, _ := st.GetByUserID(2); link != nil {
		t.Fatal("blocked link should be gone")
	}
}
