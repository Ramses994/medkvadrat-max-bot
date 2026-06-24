package reminders

// TargetPatients returns patient IDs to query: linked users intersected with allowlist.
// Empty allowlist means all linked patients.
func TargetPatients(linked, allowlist []int64) []int64 {
	if len(linked) == 0 {
		return nil
	}
	if len(allowlist) == 0 {
		out := make([]int64, len(linked))
		copy(out, linked)
		return out
	}
	set := make(map[int64]struct{}, len(allowlist))
	for _, id := range allowlist {
		set[id] = struct{}{}
	}
	var out []int64
	for _, id := range linked {
		if _, ok := set[id]; ok {
			out = append(out, id)
		}
	}
	return out
}
