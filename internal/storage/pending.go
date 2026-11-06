package storage

import (
	"database/sql"
	"strconv"
	"strings"
	"time"
)

const pendingChoiceTTL = 15 * time.Minute

// SavePendingChoice stores the ordered patient ids the user was shown.
func (s *Storage) SavePendingChoice(userID int64, phone string, patientIDs []int64, now time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO pending_choices (user_id, phone, patient_ids, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			phone = excluded.phone,
			patient_ids = excluded.patient_ids,
			created_at = excluded.created_at`,
		userID, phone, formatPatientIDs(patientIDs), now.UTC().Format(time.RFC3339))
	return err
}

// PendingChoice returns the saved list when it is younger than 15 minutes.
func (s *Storage) PendingChoice(userID int64, now time.Time) (phone string, patientIDs []int64, ok bool, err error) {
	var ids, created string
	err = s.db.QueryRow(`
		SELECT phone, patient_ids, created_at FROM pending_choices WHERE user_id = ?`, userID).
		Scan(&phone, &ids, &created)
	if err == sql.ErrNoRows {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	at, perr := time.Parse(time.RFC3339, created)
	if perr != nil || now.Sub(at) > pendingChoiceTTL {
		_ = s.DeletePendingChoice(userID)
		return "", nil, false, nil
	}
	return phone, parsePatientIDs(ids), true, nil
}

// DeletePendingChoice removes the saved list after a bind or a number change.
func (s *Storage) DeletePendingChoice(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM pending_choices WHERE user_id = ?`, userID)
	return err
}

func formatPatientIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

func parsePatientIDs(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		out = append(out, id)
	}
	return out
}
