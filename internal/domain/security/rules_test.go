package security_test

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/security"
)

// An admin's sign-in is from a new device when none of their sign-ins in the 90 days before came from it (an update of
// the same browser is the same device; a first sign-in is no alert), and impossible travel when it comes from another
// country than their last placed one sooner than 2 hours within a world region, or 6 hours across regions.
// req: SEC-31
// asvs: V6.3.5
func TestTheSignInRules(t *testing.T) {
	th := security.Defaults
	at := time.Date(2099, 3, 2, 9, 0, 0, 0, time.UTC)
	mac := security.DeviceOf("Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) Firefox/140.0")
	if mac != security.DeviceOf("Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) Firefox/141.0") {
		t.Fatal("an update of the same browser is a new device")
	}
	phone := security.DeviceOf("Mozilla/5.0 (iPhone; CPU iPhone OS 18_1) Safari/604.1")
	history := []security.SignIn{{At: at.AddDate(0, 0, -10), Device: mac, Country: "SG"}, {At: at.Add(-3 * time.Hour), Device: mac, Country: "SG"}}
	if security.NewDevice(history, security.SignIn{At: at, Device: mac}, th) {
		t.Error("a device seen 3 hours ago is new")
	}
	if !security.NewDevice(history, security.SignIn{At: at, Device: phone}, th) {
		t.Error("a device never seen is not new")
	}
	if !security.NewDevice([]security.SignIn{{At: at.AddDate(0, 0, -91), Device: phone}, {At: at.AddDate(0, 0, -1), Device: mac}}, security.SignIn{At: at, Device: phone}, th) {
		t.Error("a device last seen 91 days ago is not new")
	}
	if security.NewDevice(nil, security.SignIn{At: at, Device: phone}, th) {
		t.Error("an admin's first sign-in raised an alert")
	}

	for name, c := range map[string]struct {
		from string
		ago  time.Duration
		to   string
		want bool
	}{
		"Singapore to Kuala Lumpur in an hour":    {"SG", time.Hour, "MY", true},
		"Singapore to Kuala Lumpur in 3 hours":    {"SG", 3 * time.Hour, "MY", false},
		"Singapore to London in 5 hours":          {"SG", 5 * time.Hour, "GB", true},
		"Singapore to London in 7 hours":          {"SG", 7 * time.Hour, "GB", false},
		"the same country, minutes apart":         {"SG", time.Minute, "SG", false},
		"from a sign-in the edge did not place":   {"", time.Minute, "GB", false},
		"to a sign-in the edge did not place":     {"SG", time.Minute, "", false},
		"a code the table does not hold, 3 hours": {"SG", 3 * time.Hour, "ZZ", true},
	} {
		h := []security.SignIn{{At: at.Add(-c.ago), Device: mac, Country: c.from}}
		if got := security.ImpossibleTravel(h, security.SignIn{At: at, Device: mac, Country: c.to}, th); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
	if security.WorldRegion("sg") != "Asia" || security.WorldRegion("GB") != "Europe" || security.WorldRegion("BR") != "Americas" {
		t.Error("the world regions")
	}
}

// req: SEC-31
// asvs: V6.3.5
func TestReadThresholds(t *testing.T) {
	env := func(v string) func(string) string { return func(string) string { return v } }
	if got, err := security.ReadThresholds(env(`{"exports_per_hour":30}`)); err != nil || got.ExportsPerHour != 30 || got.DeviceDays != 90 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{"no", `{"everything":1}`, `{"travel_far_hours":1}`, `{"device_days":0}`} {
		if _, err := security.ReadThresholds(env(bad)); err == nil {
			t.Errorf("%s was read", bad)
		}
	}
}
