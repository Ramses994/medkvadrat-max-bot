package reminders

// CanNotify is the single gate before any reminder or booking-notice send.
// Until LK-1e, only Medialog DATA_W3_RASSYLKA.MESSENGER_NOTIFY=1 counts.
// hasBotConsent is reserved for a later bot-side consent that is not yet stored.
func CanNotify(messengerNotify *int, noSMS, isWaitlist, hasBotConsent bool) bool {
	if noSMS || isWaitlist {
		return false
	}
	if messengerNotify != nil && *messengerNotify == 1 {
		return true
	}
	_ = hasBotConsent
	return false
}
