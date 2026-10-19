package pack

import "time"

// InForce reports whether v is in force on d: on or after its effective_from and, when it has one, on or
// before its effective_to.
func (v Version) InForce(d time.Time) bool {
	return !d.Before(v.EffectiveFrom) && (v.EffectiveTo == nil || !d.After(*v.EffectiveTo))
}

// later reports whether a is a later version than b: by year, then by number.
func later(a, b Version) bool {
	return a.Year > b.Year || (a.Year == b.Year && a.N > b.N)
}

// At returns the version of pack in force on d among versions, which are the organisation's active ones:
// of those in force on d, the one that came into force last, and of two from the same day the later
// version, as a correction supersedes what it corrects. It never reads the clock, so a version that comes
// into force later never changes the version, or a value, chosen for an earlier date (ENG-31).
func At(versions []Version, pack string, d time.Time) (Version, bool) {
	var best Version
	found := false
	for _, v := range versions {
		if v.Pack != pack || !v.InForce(d) {
			continue
		}
		if !found || v.EffectiveFrom.After(best.EffectiveFrom) || (v.EffectiveFrom.Equal(best.EffectiveFrom) && later(v, best)) {
			best, found = v, true
		}
	}
	return best, found
}
