package storage

import (
	"database/sql"
	"time"
)

const birthBlock = 24 * time.Hour

// BirthBlocked reports a 24-hour lock after three wrong birth dates. The date is not stored.
func (s *Storage) BirthBlocked(userID int64, now time.Time) (bool, error) {
	var until string
	err := s.db.QueryRow(`SELECT blocked_until FROM birth_checks WHERE user_id = ?`, userID).Scan(&until)
	if err == sql.ErrNoRows || until == "" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	at, perr := time.Parse(time.RFC3339, until)
	if perr != nil {
		return false, nil
	}
	return now.Before(at), nil
}

// NoteBirthFailure counts a wrong date. The third failure locks linking for 24 hours.
func (s *Storage) NoteBirthFailure(userID int64, now time.Time) (bool, error) {
	_, err := s.db.Exec(`
		INSERT INTO birth_checks (user_id, failures, blocked_until) VALUES (?, 1, '')
		ON CONFLICT(user_id) DO UPDATE SET failures = failures + 1`, userID)
	if err != nil {
		return false, err
	}
	var n int
	if err := s.db.QueryRow(`SELECT failures FROM birth_checks WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return false, err
	}
	if n < 3 {
		return false, nil
	}
	until := now.UTC().Add(birthBlock).Format(time.RFC3339)
	_, err = s.db.Exec(`UPDATE birth_checks SET blocked_until = ? WHERE user_id = ?`, until, userID)
	return err == nil, err
}

// ClearBirthFailures removes the counter after a successful link.
func (s *Storage) ClearBirthFailures(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM birth_checks WHERE user_id = ?`, userID)
	return err
}
