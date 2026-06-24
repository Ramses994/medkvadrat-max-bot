package reminders

import "time"

type Kind string

const (
	KindD3 Kind = "d3"
	KindD1 Kind = "d1"
	KindH1 Kind = "h1"
)

var (
	kindOffsets = map[Kind]time.Duration{
		KindD3: 72 * time.Hour,
		KindD1: 24 * time.Hour,
		KindH1: 1 * time.Hour,
	}
	kindGrace = map[Kind]time.Duration{
		KindD3: 6 * time.Hour,
		KindD1: 6 * time.Hour,
		KindH1: 20 * time.Minute,
	}
)

// MaxGrace is the largest per-kind grace window (used to size gateway fetch window).
const MaxGrace = 6 * time.Hour

// DueKinds returns reminder kinds that should fire for appointment appt at moment now (MSK wall times).
// For each kind: target = appt - offset; send when target <= now < appt and now-target <= grace.
func DueKinds(appt, now time.Time) []Kind {
	if !appt.After(now) {
		return nil
	}
	var out []Kind
	for _, k := range []Kind{KindD3, KindD1, KindH1} {
		target := appt.Add(-kindOffsets[k])
		if now.Before(target) || !now.Before(appt) {
			continue
		}
		if now.Sub(target) <= kindGrace[k] {
			out = append(out, k)
		}
	}
	return out
}

func MoscowLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*3600)
	}
	return loc
}
