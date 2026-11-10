// Package security raises the security alerts of SEC-31 (p1-security-hardening design D7): an admin's sign-in from a new
// device, or from another country sooner than travel allows; a vendor's bank details changed; a mass export; repeated
// authorisation refusals; an audit-chain verification failure; and, from p1-assistant, the AI red team's pattern hits.
// Each alert is a locked critical notice to the Owners and the DPO (the Super admins for the installation's), a
// structured log entry Cloud Monitoring alerts on, and an audit event; one per rule, person and window, so an alert
// that keeps firing does not drown the others. The rules are pure functions of what the store already keeps; the
// thresholds are deployment settings.
package security

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The rules.
const (
	RuleNewDevice        = "new_device"
	RuleImpossibleTravel = "impossible_travel"
	RuleVendorBank       = "vendor_bank_changed"
	RuleMassExport       = "mass_export"
	RuleRefusals         = "authorisation_refusals"
	RuleAuditChain       = "audit_chain"
	RuleAI               = "ai_pattern"
)

// Thresholds are the rules' settings (FYNIQ_ALERT_THRESHOLDS).
type Thresholds struct {
	DeviceDays      int     `json:"device_days"`       // a device not seen for an admin in this many days is new
	TravelNearHours float64 `json:"travel_near_hours"` // another country of the same world region sooner than this is impossible travel
	TravelFarHours  float64 `json:"travel_far_hours"`  // another world region sooner than this is
	ExportsPerHour  int     `json:"exports_per_hour"`  // more exports or reveals than this by one person in an hour is a mass export
	RefusalsPer10   int     `json:"refusals_per_10_minutes"`
	AlertWindowMins int     `json:"alert_window_minutes"` // one alert per rule and person in this many minutes
}

// Defaults are the thresholds when the deployment sets none: coarse on purpose (design D7).
var Defaults = Thresholds{DeviceDays: 90, TravelNearHours: 2, TravelFarHours: 6, ExportsPerHour: 20, RefusalsPer10: 20, AlertWindowMins: 60}

// ReadThresholds reads FYNIQ_ALERT_THRESHOLDS, JSON of the settings to change; the rest keep their defaults.
func ReadThresholds(env func(string) string) (Thresholds, error) {
	t := Defaults
	raw := env("FYNIQ_ALERT_THRESHOLDS")
	if raw == "" {
		return t, nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return Defaults, fmt.Errorf("FYNIQ_ALERT_THRESHOLDS is not JSON of the alert settings, such as {\"exports_per_hour\":30}: %w", err)
	}
	if t.DeviceDays < 1 || t.TravelNearHours <= 0 || t.TravelFarHours < t.TravelNearHours || t.ExportsPerHour < 1 || t.RefusalsPer10 < 1 ||
		t.AlertWindowMins < 1 {
		return Defaults, fmt.Errorf("FYNIQ_ALERT_THRESHOLDS: every setting is positive, and travel_far_hours is at least travel_near_hours: %+v", t)
	}
	return t, nil
}

// Window is how long one alert of a rule about a person stands.
func (t Thresholds) Window() time.Duration { return time.Duration(t.AlertWindowMins) * time.Minute }

// SignIn is a sign-in as the rules see it: when, from which device and from which country.
type SignIn struct {
	At      time.Time
	Device  string // DeviceOf the user agent
	Country string // as the edge said; "" when it did not
}

// versions are the numbers of a user agent, which change with every update of the same browser on the same machine.
var versions = regexp.MustCompile(`\d+`)

// DeviceOf is a user agent without its version numbers: the same browser on the same machine across its updates.
func DeviceOf(userAgent string) string { return versions.ReplaceAllString(userAgent, "#") }

// earlier is history before s, the newest first.
func earlier(history []SignIn, s SignIn) []SignIn {
	var out []SignIn
	for _, h := range history {
		if h.At.Before(s.At) {
			out = append(out, h)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// NewDevice reports whether s came from a device none of the admin's sign-ins in the DeviceDays before it came from.
// An admin's first sign-in is no alert: every device is new then.
func NewDevice(history []SignIn, s SignIn, t Thresholds) bool {
	before := earlier(history, s)
	if len(before) == 0 {
		return false
	}
	since := s.At.AddDate(0, 0, -t.DeviceDays)
	for _, h := range before {
		if !h.At.Before(since) && h.Device == s.Device {
			return false
		}
	}
	return true
}

// ImpossibleTravel reports whether s came from another country than the admin's last sign-in that said where it came
// from, sooner than travel allows: TravelNearHours within one world region, TravelFarHours across regions. A sign-in the
// edge did not place is never impossible travel.
func ImpossibleTravel(history []SignIn, s SignIn, t Thresholds) bool {
	if s.Country == "" {
		return false
	}
	for _, h := range earlier(history, s) {
		if h.Country == "" {
			continue
		}
		if h.Country == s.Country {
			return false
		}
		limit := t.TravelFarHours
		if r := WorldRegion(h.Country); r != "" && r == WorldRegion(s.Country) {
			limit = t.TravelNearHours
		}
		return s.At.Sub(h.At) < time.Duration(limit*float64(time.Hour))
	}
	return false
}
