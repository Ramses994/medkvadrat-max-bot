package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Pure-Go SQLite: не требует CGO, собирается в статический бинарник для Alpine.
	_ "modernc.org/sqlite"
)

// UserLink — одна запись: MAX-пользователь привязан к пациенту Medialog.
type UserLink struct {
	UserID     int64  // user_id из MAX
	PatientID  int64  // PATIENTS_ID из Medialog
	Phone      string // нормализованный, например "79991234567"
	FullName   string // кэшируем, чтобы приветствовать по имени без лишнего хождения в gateway
	CreatedAt  time.Time
	VerifiedAt string // set only after a MAX contact confirms the phone; empty means unverified
}

func (u *UserLink) Verified() bool {
	return u != nil && u.PatientID > 0 && strings.TrimSpace(u.VerifiedAt) != ""
}

type Storage struct {
	db           *sql.DB
	expireFail   error
	omitRevision bool
}

func New(path string) (*Storage, error) {
	// Создаём директорию под файл БД, если её ещё нет
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("создание директории %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	// modernc.org/sqlite не любит много параллельных write-соединений
	db.SetMaxOpenConns(1)

	schema := `
	CREATE TABLE IF NOT EXISTS user_links (
		user_id    INTEGER PRIMARY KEY,
		patient_id INTEGER NOT NULL,
		phone      TEXT NOT NULL,
		full_name  TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_user_links_patient ON user_links(patient_id);

	CREATE TABLE IF NOT EXISTS reminder_log (
		planning_id INTEGER NOT NULL,
		kind        TEXT    NOT NULL,
		sent_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (planning_id, kind)
	);

	CREATE TABLE IF NOT EXISTS kv (
		k TEXT PRIMARY KEY,
		v TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS notice_outbox (
		planning_id INTEGER PRIMARY KEY,
		patient_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		branch_id INTEGER NOT NULL DEFAULT 0,
		branch_code TEXT NOT NULL DEFAULT '',
		doctor_name TEXT NOT NULL DEFAULT '',
		service TEXT NOT NULL DEFAULT '',
		date_consultation TEXT NOT NULL DEFAULT '',
		messenger_notify INTEGER,
		no_sms_notify INTEGER NOT NULL DEFAULT 0,
		is_waitlist INTEGER NOT NULL DEFAULT 0,
		appt_version TEXT NOT NULL DEFAULT '',
		drop_reason TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT '',
		attempts INTEGER NOT NULL DEFAULT 0,
		next_try_at DATETIME NOT NULL,
		status TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_notice_outbox_pending ON notice_outbox(status, next_try_at);

	CREATE TABLE IF NOT EXISTS pending_choices (
		user_id INTEGER PRIMARY KEY,
		phone TEXT NOT NULL,
		patient_ids TEXT NOT NULL,
		created_at TEXT NOT NULL,
		step TEXT NOT NULL DEFAULT 'choose',
		selected_id INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS birth_checks (
		user_id INTEGER PRIMARY KEY,
		failures INTEGER NOT NULL DEFAULT 0,
		blocked_until TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS consent_prompts (
		nonce TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		patient_id INTEGER NOT NULL,
		kind TEXT NOT NULL,
		text_version TEXT NOT NULL,
		created_at TEXT NOT NULL,
		used_at TEXT,
		revision INTEGER
	);
	`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("создание схемы: %w", err)
	}
	_, _ = db.Exec(`ALTER TABLE notice_outbox ADD COLUMN is_waitlist INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE notice_outbox ADD COLUMN appt_version TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE notice_outbox ADD COLUMN drop_reason TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE notice_outbox ADD COLUMN created_at DATETIME NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE user_links ADD COLUMN verified_at TEXT`)
	_, _ = db.Exec(`ALTER TABLE pending_choices ADD COLUMN step TEXT NOT NULL DEFAULT 'choose'`)
	_, _ = db.Exec(`ALTER TABLE pending_choices ADD COLUMN selected_id INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE consent_prompts ADD COLUMN revision INTEGER`)

	return &Storage{db: db}, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

// GetByUserID — вернёт nil, nil если связки нет (важно: не ошибка).
func (s *Storage) GetByUserID(userID int64) (*UserLink, error) {
	row := s.db.QueryRow(`
		SELECT user_id, patient_id, phone, full_name, created_at, verified_at
		FROM user_links WHERE user_id = ?`, userID)

	var u UserLink
	var verified sql.NullString
	err := row.Scan(&u.UserID, &u.PatientID, &u.Phone, &u.FullName, &u.CreatedAt, &verified)
	if err == nil && verified.Valid {
		u.VerifiedAt = verified.String
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Link — upsert: при повторной привязке того же user_id перезаписываем.
func (s *Storage) Link(userID, patientID int64, phone, fullName string) error {
	return s.link(userID, patientID, phone, fullName, "")
}

// LinkVerified records a phone confirmed by a MAX contact.
func (s *Storage) LinkVerified(userID, patientID int64, phone, fullName string, at time.Time) error {
	if at.IsZero() {
		at = time.Now()
	}
	return s.link(userID, patientID, phone, fullName, at.UTC().Format(time.RFC3339))
}

func (s *Storage) link(userID, patientID int64, phone, fullName, verifiedAt string) error {
	_, err := s.db.Exec(`
		INSERT INTO user_links (user_id, patient_id, phone, full_name, verified_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			patient_id = excluded.patient_id,
			phone = excluded.phone,
			full_name = excluded.full_name,
			verified_at = excluded.verified_at`,
		userID, patientID, phone, fullName, nullIfEmpty(verifiedAt))
	return err
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// Unlink — на будущее: команда /logout, смена профиля и т.п.
func (s *Storage) Unlink(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM user_links WHERE user_id = ?`, userID)
	return err
}

// IsLinked reports whether this MAX user is currently bound to this patient.
func (s *Storage) IsLinked(userID, patientID int64) (bool, error) {
	if userID <= 0 || patientID <= 0 {
		return false, nil
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM user_links WHERE user_id = ? AND patient_id = ? AND verified_at IS NOT NULL AND verified_at != ''`, userID, patientID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// UsersByPatientID returns all MAX users linked to a Medialog patient (family accounts).
func (s *Storage) UsersByPatientID(patientID int64) ([]UserLink, error) {
	if patientID <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT user_id, patient_id, phone, full_name, created_at, verified_at
		FROM user_links WHERE patient_id = ? AND verified_at IS NOT NULL AND verified_at != ''`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UserLink
	for rows.Next() {
		var u UserLink
		var verified sql.NullString
		if err := rows.Scan(&u.UserID, &u.PatientID, &u.Phone, &u.FullName, &u.CreatedAt, &verified); err != nil {
			return nil, err
		}
		if verified.Valid {
			u.VerifiedAt = verified.String
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// DistinctPatientIDs returns all patient_id values from user_links.
func (s *Storage) DistinctPatientIDs() ([]int64, error) {
	rows, err := s.db.Query(`SELECT DISTINCT patient_id FROM user_links WHERE patient_id > 0 AND verified_at IS NOT NULL AND verified_at != '' ORDER BY patient_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Storage) WasReminderSent(planningID int64, kind string) (bool, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT 1 FROM reminder_log WHERE planning_id = ? AND kind = ? LIMIT 1`,
		planningID, kind).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Storage) MarkReminderSent(planningID int64, kind string) error {
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO reminder_log (planning_id, kind) VALUES (?, ?)`,
		planningID, kind)
	return err
}
