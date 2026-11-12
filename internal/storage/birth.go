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
// remaining is how many attempts are left before the block (0 when blocked).
func (s *Storage) NoteBirthFailure(userID int64, now time.Time) (remaining int, blocked bool, err error) {
	var n int
	var until string
	err = s.db.QueryRow(`SELECT failures, blocked_until FROM birth_checks WHERE user_id = ?`, userID).Scan(&n, &until)
	if err == sql.ErrNoRows {
		_, err = s.db.Exec(`INSERT INTO birth_checks (user_id, failures, blocked_until) VALUES (?, 1, '')`, userID)
		return 2, false, err
	}
	if err != nil {
		return 0, false, err
	}
	if until != "" {
		at, perr := time.Parse(time.RFC3339, until)
		if perr == nil && !now.Before(at) {
			_, err = s.db.Exec(`UPDATE birth_checks SET failures = 1, blocked_until = '' WHERE user_id = ?`, userID)
			return 2, false, err
		}
	}
	n++
	if n < 3 {
		_, err = s.db.Exec(`UPDATE birth_checks SET failures = ? WHERE user_id = ?`, n, userID)
		return 3 - n, false, err
	}
	blockedUntil := now.UTC().Add(birthBlock).Format(time.RFC3339)
	_, err = s.db.Exec(`UPDATE birth_checks SET failures = ?, blocked_until = ? WHERE user_id = ?`, n, blockedUntil, userID)
	return 0, err == nil, err
}

// ClearBirthFailures removes the counter after a successful link.
func (s *Storage) ClearBirthFailures(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM birth_checks WHERE user_id = ?`, userID)
	return err
}
