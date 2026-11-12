package reminders

// CanNotify is the only gate before a reminder or a «Вы записаны» notice.
// A bot revocation beats the questionnaire. Direct answers do not use this.
func CanNotify(messengerNotify *int, consentService string, noSMS, isWaitlist bool) bool {
	if noSMS || isWaitlist {
		return false
	}
	if consentService == "revoked" {
		return false
	}
	if messengerNotify != nil && *messengerNotify == 1 {
		return true
	}
	if messengerNotify != nil && *messengerNotify == 0 {
		return false
	}
	return consentService == "active"
}
