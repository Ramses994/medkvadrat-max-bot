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

// NoteBirthFailure counts a wrong date. The third failure within 24 hours of the first locks linking
// for 24 hours. The counter belongs to the MAX account: linking or unlinking a profile does not reset it.
// remaining is how many attempts are left before the block (0 when blocked).
func (s *Storage) NoteBirthFailure(userID int64, now time.Time) (remaining int, blocked bool, err error) {
	stamp := now.UTC().Format(time.RFC3339)
	var n int
	var until, first string
	err = s.db.QueryRow(`SELECT failures, blocked_until, first_failure_at FROM birth_checks WHERE user_id = ?`, userID).Scan(&n, &until, &first)
	if err == sql.ErrNoRows {
		_, err = s.db.Exec(`INSERT INTO birth_checks (user_id, failures, blocked_until, first_failure_at) VALUES (?, 1, '', ?)`, userID, stamp)
		return 2, false, err
	}
	if err != nil {
		return 0, false, err
	}
	restart := false
	if until != "" {
		at, perr := time.Parse(time.RFC3339, until)
		restart = perr == nil && !now.Before(at)
	} else if first != "" {
		at, perr := time.Parse(time.RFC3339, first)
		restart = perr == nil && !now.Before(at.Add(birthBlock))
	}
	if restart {
		_, err = s.db.Exec(`UPDATE birth_checks SET failures = 1, blocked_until = '', first_failure_at = ? WHERE user_id = ?`, stamp, userID)
		return 2, false, err
	}
	if first == "" {
		first = stamp
	}
	n++
	if n < 3 {
		_, err = s.db.Exec(`UPDATE birth_checks SET failures = ?, first_failure_at = ? WHERE user_id = ?`, n, first, userID)
		return 3 - n, false, err
	}
	blockedUntil := now.UTC().Add(birthBlock).Format(time.RFC3339)
	_, err = s.db.Exec(`UPDATE birth_checks SET failures = ?, blocked_until = ?, first_failure_at = ? WHERE user_id = ?`, n, blockedUntil, first, userID)
	return 0, err == nil, err
}
