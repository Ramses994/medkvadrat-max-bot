package storage

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func legacyDB(t *testing.T, stmts ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bot.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return path
}

func TestNew_FailsWhenBirthCounterColumnCannotBeAdded(t *testing.T) {
	path := legacyDB(t, `CREATE VIEW birth_checks AS SELECT 0 AS user_id, 0 AS failures, '' AS blocked_until`)
	st, err := New(path)
	if err == nil {
		st.Close()
		t.Fatal("storage opened without birth_checks.first_failure_at")
	}
	if !strings.Contains(err.Error(), "birth_checks") || !strings.Contains(err.Error(), "first_failure_at") {
		t.Fatalf("err=%v", err)
	}
}

func TestNew_FailsWithoutConsentButtonColumn(t *testing.T) {
	path := legacyDB(t, `CREATE TABLE consent_prompts (nonce TEXT PRIMARY KEY, user_id INTEGER, patient_id INTEGER, kind TEXT, text_version TEXT, created_at TEXT)`)
	st, err := New(path)
	if err == nil {
		st.Close()
		t.Fatal("storage opened without consent_prompts.used_at")
	}
	if !strings.Contains(err.Error(), "consent_prompts") || !strings.Contains(err.Error(), "used_at") {
		t.Fatalf("err=%v", err)
	}
}

func TestNew_MigratesLegacyBirthCounterAndReopens(t *testing.T) {
	path := legacyDB(t,
		`CREATE TABLE birth_checks (user_id INTEGER PRIMARY KEY, failures INTEGER NOT NULL DEFAULT 0, blocked_until TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO birth_checks (user_id, failures, blocked_until) VALUES (42, 2, '')`)
	st, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = New(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer st.Close()
	matched, _, blocked, err := st.BirthAttempt(42, time.Now(), func() bool { return false })
	if err != nil || matched || !blocked {
		t.Fatalf("legacy failures must count: matched=%v blocked=%v err=%v", matched, blocked, err)
	}
}

func TestBirthAttempt_WriteErrorSkipsTheComparison(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, q := range []string{
		`CREATE TRIGGER birth_no_insert BEFORE INSERT ON birth_checks BEGIN SELECT RAISE(ABORT, 'disk'); END`,
		`CREATE TRIGGER birth_no_update BEFORE UPDATE ON birth_checks BEGIN SELECT RAISE(ABORT, 'disk'); END`,
	} {
		if _, err := st.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	called := false
	matched, _, _, err := st.BirthAttempt(42, time.Now(), func() bool { called = true; return true })
	if err == nil || matched || called {
		t.Fatalf("err=%v matched=%v compared=%v", err, matched, called)
	}
}
