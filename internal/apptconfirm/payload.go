package apptconfirm

import (
	"strconv"
	"strings"
)

// ParsePayload splits "action:planning_id" or "action:planning_id:appt_version".
func ParsePayload(payload string) (action string, planningID int64, version string, ok bool) {
	payload = strings.TrimSpace(payload)
	parts := strings.Split(payload, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return "", 0, "", false
	}
	action = strings.ToLower(parts[0])
	if action == "" {
		return "", 0, "", false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return "", 0, "", false
	}
	if len(parts) == 3 {
		version = parts[2]
	}
	return action, id, version, true
}

func ActionToStatus(action string) (status string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "confirm":
		return "confirmed", true
	case "decline":
		return "declined", true
	case "reschedule":
		return "reschedule", true
	default:
		return "", false
	}
}
