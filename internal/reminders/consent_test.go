package reminders

import "testing"

func TestCanNotify_Matrix(t *testing.T) {
	one := 1
	zero := 0
	cases := []struct {
		name    string
		notify  *int
		consent string
		noSMS   bool
		wait    bool
		want    bool
	}{
		{name: "questionnaire yes", notify: &one, consent: "none", want: true},
		{name: "questionnaire yes and active", notify: &one, consent: "active", want: true},
		{name: "revocation beats questionnaire", notify: &one, consent: "revoked", want: false},
		{name: "questionnaire no", notify: &zero, consent: "active", want: false},
		{name: "questionnaire no and revoked", notify: &zero, consent: "revoked", want: false},
		{name: "no questionnaire and none", notify: nil, consent: "none", want: false},
		{name: "no questionnaire and active", notify: nil, consent: "active", want: true},
		{name: "no questionnaire and revoked", notify: nil, consent: "revoked", want: false},
		{name: "no sms", notify: &one, consent: "active", noSMS: true, want: false},
		{name: "waitlist", notify: &one, consent: "active", wait: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanNotify(tc.notify, tc.consent, tc.noSMS, tc.wait); got != tc.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}
