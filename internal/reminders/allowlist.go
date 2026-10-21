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

// ReminderAudience is linked patients ∩ link allowlist ∩ reminder allowlist (if set).
// An empty link allowlist allows nobody. An empty reminder allowlist does not widen the link allowlist.
func ReminderAudience(linked, linkAllow, reminderAllow []int64) []int64 {
	out := intersectIDs(linked, linkAllow)
	if len(reminderAllow) == 0 {
		return out
	}
	return intersectIDs(out, reminderAllow)
}

// BookingAudience requires both allowlists. An empty reminder allowlist means nobody.
func BookingAudience(linked, linkAllow, reminderAllow []int64) []int64 {
	if len(reminderAllow) == 0 {
		return nil
	}
	return ReminderAudience(linked, linkAllow, reminderAllow)
}

func intersectIDs(ids, allow []int64) []int64 {
	if len(ids) == 0 || len(allow) == 0 {
		return nil
	}
	set := make(map[int64]struct{}, len(allow))
	for _, id := range allow {
		set[id] = struct{}{}
	}
	var out []int64
	for _, id := range ids {
		if _, ok := set[id]; ok {
			out = append(out, id)
		}
	}
	return out
}
