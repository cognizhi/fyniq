package checks

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// pendingUpdate is a statutory pack's update the organisation has not accepted (ENG-33, G4-103): the version of the
// pack it activated that is in force on today, and a later version loaded and in force on today too that it has not
// activated. later is "" when there is none, or the organisation activated no version of the pack (it then reads the
// release's own, which is current).
func pendingUpdate(ctx context.Context, u storage.UnitOfWork, pack string, today time.Time) (current, later string, err error) {
	versions, err := u.Packs().Versions(ctx)
	if err != nil {
		return "", "", err
	}
	acts, err := u.Packs().Activations(ctx)
	if err != nil {
		return "", "", err
	}
	active := map[string]bool{}
	for _, a := range acts {
		active[a.VersionID] = true
	}
	inForce := func(v storage.PackVersion) bool {
		return !v.EffectiveFrom.After(today) && (v.EffectiveTo == nil || !v.EffectiveTo.Before(today))
	}
	var cur *storage.PackVersion
	for i, v := range versions {
		if v.Pack == pack && active[v.ID] && inForce(v) && (cur == nil || newer(v, *cur)) {
			cur = &versions[i]
		}
	}
	if cur == nil {
		return "", "", nil
	}
	var next *storage.PackVersion // the newest of those pending
	for i, v := range versions {
		if v.Pack == pack && !active[v.ID] && inForce(v) && newer(v, *cur) && (next == nil || newer(v, *next)) {
			next = &versions[i]
		}
	}
	if next == nil {
		return cur.ID, "", nil
	}
	return cur.ID, next.ID, nil
}

// basedOnPending is a check's result while a pack it reads has an update the organisation has not accepted: not
// assessed, its figures labelled with the version they would be based on, never an all clear (ENG-33, G4-103).
func basedOnPending(pack, current, later string) Result {
	return Result{Status: NotAssessed, Needs: "the update of " + pack + " to " + later + " activated", Setting: "Settings › Statutory packs",
		Detail: "Not assessed: based on " + current + "; update pending: " + later + " is in force and not activated"}
}

// newer says whether a is later than b: in force from a later day, or from the same day with a later number, such as
// sg-gst@2026.2 after sg-gst@2026.1.
func newer(a, b storage.PackVersion) bool {
	if !a.EffectiveFrom.Equal(b.EffectiveFrom) {
		return a.EffectiveFrom.After(b.EffectiveFrom)
	}
	ay, an := number(a.ID)
	by, bn := number(b.ID)
	return ay > by || (ay == by && an > bn)
}

// number is a version's year and number, from its ID: sg-gst@2026.2 is 2026 and 2.
func number(id string) (int, int) {
	_, v, _ := strings.Cut(id, "@")
	y, n, _ := strings.Cut(v, ".")
	year, _ := strconv.Atoi(y)
	num, _ := strconv.Atoi(n)
	return year, num
}
