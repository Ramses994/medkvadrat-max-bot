package apptconfirm

import (
	"strconv"
	"strings"
)

// ParsePayload splits "action:planning_id" callback payloads.
func ParsePayload(payload string) (action string, planningID int64, ok bool) {
	payload = strings.TrimSpace(payload)
	i := strings.IndexByte(payload, ':')
	if i <= 0 || i >= len(payload)-1 {
		return "", 0, false
	}
	action = strings.ToLower(payload[:i])
	id, err := strconv.ParseInt(payload[i+1:], 10, 64)
	if err != nil || id <= 0 {
		return "", 0, false
	}
	return action, id, true
}

// ActionToStatus maps button action to gateway confirmation status.
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
