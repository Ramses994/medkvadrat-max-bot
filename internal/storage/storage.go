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