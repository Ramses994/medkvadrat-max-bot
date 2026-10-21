package reminders

import "strings"

// CanNotify is the single gate before any reminder or booking-notice send.
// Until LK-1e, only Medialog DATA_W3_RASSYLKA.MESSENGER_NOTIFY=1 counts.
// hasBotConsent is reserved for a later bot-side consent that is not yet stored.
func CanNotify(messengerNotify *int, noSMS bool, service string, hasBotConsent bool) bool {
	if noSMS || isWaitlist(service) {
		return false
	}
	if messengerNotify != nil && *messengerNotify == 1 {
		return true
	}
	_ = hasBotConsent
	return false
}

func isWaitlist(service string) bool {
	return strings.Contains(strings.ToLower(service), "ожидан")
}
