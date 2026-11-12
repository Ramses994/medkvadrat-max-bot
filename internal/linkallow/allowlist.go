package linkallow

// Set is a patient ID allowlist for bot linking and lab access.
// An empty set means nobody. Everyone is only the explicit "*" config.
type Set struct {
	all bool
	ids map[int64]struct{}
}

func New(ids []int64) Set {
	s := Set{ids: make(map[int64]struct{}, len(ids))}
	for _, id := range ids {
		if id > 0 {
			s.ids[id] = struct{}{}
		}
	}
	return s
}

// Everyone allows every positive patient id. It is not the default.
func Everyone() Set {
	return Set{all: true, ids: map[int64]struct{}{}}
}

func (s Set) All() bool { return s.all }

func (s Set) Empty() bool { return !s.all && len(s.ids) == 0 }

func (s Set) Allowed(patientID int64) bool {
	if patientID <= 0 {
		return false
	}
	if s.all {
		return true
	}
	_, ok := s.ids[patientID]
	return ok
}

func (s Set) IDs() []int64 {
	out := make([]int64, 0, len(s.ids))
	for id := range s.ids {
		out = append(out, id)
	}
	return out
}
