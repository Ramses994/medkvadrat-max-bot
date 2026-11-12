package storage

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"
)

const consentPromptTTL = 30 * 24 * time.Hour

// ConsentPrompt is one shown consent screen. It stores no message text.
type ConsentPrompt struct {
	Nonce       string
	UserID      int64
	PatientID   int64
	Kind        string
	TextVersion string
	CreatedAt   time.Time
}

// SaveConsentPrompt records a one-time button for the text version that was shown.
func (s *Storage) SaveConsentPrompt(userID, patientID int64, kind, version string, now time.Time) (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(buf[:])
	_, err := s.db.Exec(`
		INSERT INTO consent_prompts (nonce, user_id, patient_id, kind, text_version, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		nonce, userID, patientID, kind, version, now.UTC().Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	return nonce, nil
}

// LookupConsentPrompt reads a prompt without using it. used is true when used_at is set.
func (s *Storage) LookupConsentPrompt(nonce string) (*ConsentPrompt, bool, error) {
	var p ConsentPrompt
	var created string
	var used sql.NullString
	err := s.db.QueryRow(`
		SELECT nonce, user_id, patient_id, kind, text_version, created_at, used_at
		FROM consent_prompts WHERE nonce = ?`, nonce).
		Scan(&p.Nonce, &p.UserID, &p.PatientID, &p.Kind, &p.TextVersion, &created, &used)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	at, perr := time.Parse(time.RFC3339, created)
	if perr != nil {
		return nil, false, nil
	}
	p.CreatedAt = at
	return &p, used.Valid && strings.TrimSpace(used.String) != "", nil
}

// MarkConsentPromptUsed sets used_at once. The second call returns false.
func (s *Storage) MarkConsentPromptUsed(nonce string, now time.Time) (bool, error) {
	res, err := s.db.Exec(`
		UPDATE consent_prompts SET used_at = ?
		WHERE nonce = ? AND used_at IS NULL`, now.UTC().Format(time.RFC3339), nonce)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ExpireConsentPrompts invalidates unused screens after a revocation.
func (s *Storage) ExpireConsentPrompts(userID int64, kind string, now time.Time) error {
	_, err := s.db.Exec(`
		UPDATE consent_prompts SET used_at = ?
		WHERE user_id = ? AND kind = ? AND used_at IS NULL`,
		now.UTC().Format(time.RFC3339), userID, kind)
	return err
}
