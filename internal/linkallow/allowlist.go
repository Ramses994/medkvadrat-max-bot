package linkallow

// Set is a patient ID allowlist for bot linking and lab access.
// Empty set means nobody is allowed (test mode for all).
type Set map[int64]struct{}

func New(ids []int64) Set {
	s := make(Set, len(ids))
	for _, id := range ids {
		if id > 0 {
			s[id] = struct{}{}
		}
	}
	return s
}

func (s Set) Allowed(patientID int64) bool {
	if patientID <= 0 || len(s) == 0 {
		return false
	}
	_, ok := s[patientID]
	return ok
}