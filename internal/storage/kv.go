package storage

import "database/sql"

const BookingNoticeSinceKey = "booking_notice_since"

func (s *Storage) GetKV(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT v FROM kv WHERE k = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Storage) SetKV(key, value string) error {
	_, err := s.db.Exec(`
INSERT INTO kv (k, v) VALUES (?, ?)
ON CONFLICT(k) DO UPDATE SET v = excluded.v`, key, value)
	return err
}
