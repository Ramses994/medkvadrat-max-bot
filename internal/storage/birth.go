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

// BirthAttempt checks a birth date against the MAX account's wrong-date counter. The failure is written
// in a transaction before match runs and is rolled back only when the date matched, so a counter that
// cannot be read or written returns an error and the date is never compared.
// remaining is how many attempts are left before the block (0 when blocked).
func (s *Storage) BirthAttempt(userID int64, now time.Time, match func() bool) (matched bool, remaining int, blocked bool, err error) {
	if s.birthFail != nil {
		return false, 0, false, s.birthFail
	}
	tx, err := s.db.Begin()
	if err != nil {
		return false, 0, false, err
	}
	remaining, blocked, err = noteBirthFailure(tx, userID, now)
	if err != nil {
		_ = tx.Rollback()
		return false, 0, false, err
	}
	if match() {
		if err := tx.Rollback(); err != nil {
			return false, 0, false, err
		}
		return true, 0, false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, 0, false, err
	}
	return false, remaining, blocked, nil
}

// FailBirthCounter makes every BirthAttempt return err. Tests use it for a counter that cannot be written.
func (s *Storage) FailBirthCounter(err error) { s.birthFail = err }

// noteBirthFailure counts a wrong date. The third failure within 24 hours of the first locks linking
// for 24 hours. The counter belongs to the MAX account: linking or unlinking a profile does not reset it.
func noteBirthFailure(tx *sql.Tx, userID int64, now time.Time) (remaining int, blocked bool, err error) {
	stamp := now.UTC().Format(time.RFC3339)
	var n int
	var until, first string
	err = tx.QueryRow(`SELECT failures, blocked_until, first_failure_at FROM birth_checks WHERE user_id = ?`, userID).Scan(&n, &until, &first)
	if err == sql.ErrNoRows {
		_, err = tx.Exec(`INSERT INTO birth_checks (user_id, failures, blocked_until, first_failure_at) VALUES (?, 1, '', ?)`, userID, stamp)
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
		_, err = tx.Exec(`UPDATE birth_checks SET failures = 1, blocked_until = '', first_failure_at = ? WHERE user_id = ?`, stamp, userID)
		return 2, false, err
	}
	if first == "" {
		first = stamp
	}
	n++
	if n < 3 {
		_, err = tx.Exec(`UPDATE birth_checks SET failures = ?, first_failure_at = ? WHERE user_id = ?`, n, first, userID)
		return 3 - n, false, err
	}
	blockedUntil := now.UTC().Add(birthBlock).Format(time.RFC3339)
	_, err = tx.Exec(`UPDATE birth_checks SET failures = ?, blocked_until = ?, first_failure_at = ? WHERE user_id = ?`, n, blockedUntil, first, userID)
	return 0, err == nil, err
}
