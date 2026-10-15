package storage

import (
	"fmt"
	"strings"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/linkallow"
)

// PurgeLinksOutsideAllowlist removes user_links not on the allowlist (including pending_selection).
// Returns the number of deleted rows.
func (s *Storage) PurgeLinksOutsideAllowlist(allow linkallow.Set) (int64, error) {
	if len(allow) == 0 {
		res, err := s.db.Exec(`DELETE FROM user_links`)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	ids := make([]string, 0, len(allow))
	args := make([]any, 0, len(allow))
	for id := range allow {
		ids = append(ids, "?")
		args = append(args, id)
	}
	q := fmt.Sprintf(`DELETE FROM user_links WHERE patient_id = 0 OR patient_id NOT IN (%s)`, strings.Join(ids, ","))
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
