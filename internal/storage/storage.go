package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// Pure-Go SQLite: не требует CGO, собирается в статический бинарник для Alpine.
	_ "modernc.org/sqlite"
)

// UserLink — одна запись: MAX-пользователь привязан к пациенту Medialog.
type UserLink struct {
	UserID    int64     // user_id из MAX
	PatientID int64     // PATIENTS_ID из Medialog
	Phone     string    // нормализованный, например "79991234567"
	FullName  string    // кэшируем, чтобы приветствовать по имени без лишнего хождения в gateway
	CreatedAt time.Time
}

type Storage struct {
	db *sql.DB
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
	`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("создание схемы: %w", err)
	}

	return &Storage{db: db}, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

// GetByUserID — вернёт nil, nil если связки нет (важно: не ошибка).
func (s *Storage) GetByUserID(userID int64) (*UserLink, error) {
	row := s.db.QueryRow(`
		SELECT user_id, patient_id, phone, full_name, created_at
		FROM user_links WHERE user_id = ?`, userID)

	var u UserLink
	err := row.Scan(&u.UserID, &u.PatientID, &u.Phone, &u.FullName, &u.CreatedAt)
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
	_, err := s.db.Exec(`
		INSERT INTO user_links (user_id, patient_id, phone, full_name)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			patient_id = excluded.patient_id,
			phone = excluded.phone,
			full_name = excluded.full_name`,
		userID, patientID, phone, fullName)
	return err
}

// Unlink — на будущее: команда /logout, смена профиля и т.п.
func (s *Storage) Unlink(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM user_links WHERE user_id = ?`, userID)
	return err
}

// UsersByPatientID returns all MAX users linked to a Medialog patient (family accounts).
func (s *Storage) UsersByPatientID(patientID int64) ([]UserLink, error) {
	if patientID <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT user_id, patient_id, phone, full_name, created_at
		FROM user_links WHERE patient_id = ?`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UserLink
	for rows.Next() {
		var u UserLink
		if err := rows.Scan(&u.UserID, &u.PatientID, &u.Phone, &u.FullName, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// DistinctPatientIDs returns all patient_id values from user_links.
func (s *Storage) DistinctPatientIDs() ([]int64, error) {
	rows, err := s.db.Query(`SELECT DISTINCT patient_id FROM user_links ORDER BY patient_id`)
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