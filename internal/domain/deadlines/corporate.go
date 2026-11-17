package deadlines

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/corporate"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The corporate profile's lodgements (FR-CS-01, FR-CS-02; p1-company-secretary task 2.4, design D2): each change ACRA
// must be told of, due by the sg-acra pack, done on its own record once a version with the day it was lodged is
// recorded with the acknowledgement.
const (
	ObligationOfficerAppointment = "register_officer_appointment" // an officer's appointment, lodged within 14 days (s173A)
	ObligationOfficerCessation   = "register_officer_cessation"   // an officer's cessation, lodged within 14 days (s173A)
	ObligationDetailChange       = "register_detail_change"       // a change of the registered office or details, within 14 days (s143)
	ObligationNomineeInforms     = "register_nominee_informs"     // a nominee tells the company within 30 days (s386AL)
	ObligationNomineeEntry       = "register_nominee_entry"       // the company's register of nominees within 7 days
	ObligationNomineeCentral     = "register_nominee_central"     // ACRA's central register within 2 business days after
	ObligationACRAPack           = "sg-acra"                      // no sg-acra version in force: the lodgements are not assessed
)

// The pack's rule each obligation reads.
var acraRule = map[string]string{ObligationOfficerAppointment: "officer_change", ObligationOfficerCessation: "officer_change",
	ObligationDetailChange: "office_change", ObligationNomineeInforms: "nominee_informs", ObligationNomineeEntry: "nominee_register",
	ObligationNomineeCentral: "nominee_central"}

// corporateItems are the corporate profile's lodgements open on today: each officer appointed after incorporation (the
// first detail's day) or ceased, each change of the details after the first, and each nominee entry's steps, with no
// day lodged recorded. The days come from the sg-acra version in force, business days skipping cal's public holidays.
// An event before the version's first day has no rule in it, so no item, as the calendar's own rules start (design D5).
// With records to lodge and no version in force, one item is not assessed, naming the pack; with no detail recorded,
// the officers' appointments are not assessed, naming the details.
func corporateItems(ctx context.Context, u storage.UnitOfWork, today time.Time, cal pack.Version) ([]Item, error) {
	details, err := corporate.Details(ctx, u)
	if err != nil {
		return nil, err
	}
	officers, err := corporate.Officers(ctx, u)
	if err != nil {
		return nil, err
	}
	nominees, err := corporate.Nominees(ctx, u)
	if err != nil {
		return nil, err
	}
	type event struct {
		obligation, id, title string
		on                    time.Time
	}
	var events []event
	var incorporated *time.Time
	for _, d := range details {
		if d.Doc.Status == storage.DocumentPosted && (incorporated == nil || d.Value.From.Before(*incorporated)) {
			from := d.Value.From
			incorporated = &from
		}
	}
	for _, d := range details {
		if d.Doc.Status == storage.DocumentPosted && d.Value.Lodged == nil && incorporated != nil && d.Value.From.After(*incorporated) {
			events = append(events, event{ObligationDetailChange, d.Doc.ID.String(), "Lodge the change of " + d.Value.Name + "'s details with ACRA", d.Value.From})
		}
	}
	var out []Item
	ids := slices.Collect(maps.Keys(officers))
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, id := range ids {
		cur := equity.InForce(officers[id])
		if cur == nil {
			continue
		}
		o, key := cur.Value, id.String()
		role := roleWords[o.Role]
		switch {
		case o.AppointmentLodged != nil:
		case incorporated == nil:
			out = append(out, Item{Key: ObligationOfficerAppointment + ":" + key, Obligation: ObligationOfficerAppointment, Area: pack.AreaCorporate,
				Title: "Lodge " + o.Name + "'s appointment as " + role + " with ACRA", Status: NotAssessed, Register: true, Link: profileLink,
				Reason: "The corporate profile has no details recorded, so the day of incorporation is not known: record them from incorporation"})
		case o.Appointed.After(*incorporated):
			events = append(events, event{ObligationOfficerAppointment, key, "Lodge " + o.Name + "'s appointment as " + role + " with ACRA", o.Appointed})
		}
		if o.Ceased != nil && o.CessationLodged == nil {
			events = append(events, event{ObligationOfficerCessation, key, "Lodge " + o.Name + "'s cessation as " + role + " with ACRA", *o.Ceased})
		}
	}
	for root, versions := range nominees {
		cur := equity.InForce(versions)
		if cur == nil || cur.Value.Ceased != nil {
			continue
		}
		n, id := cur.Value, root.String()
		what := "nominee " + n.Kind + " for " + n.Nominator
		if n.Informed == nil {
			events = append(events, event{ObligationNomineeInforms, id, "The " + what + " tells the company they are one", n.Since})
		}
		switch {
		case n.Register == nil:
			events = append(events, event{ObligationNomineeEntry, id, "Enter the " + what + " in the register of nominee " + n.Kind + "s", n.Since})
		case n.Central == nil:
			events = append(events, event{ObligationNomineeCentral, id, "File the " + what + " with ACRA's central register of nominees", *n.Register})
		}
	}
	if len(events) == 0 {
		return out, nil
	}
	acra, ok, err := gst.PackOn(ctx, u, "sg-acra", today)
	if err != nil {
		return nil, err
	}
	if !ok {
		return append(out, Item{Key: ObligationACRAPack + ":" + today.Format(time.DateOnly), Obligation: ObligationACRAPack, Area: pack.AreaCorporate,
			Title: "The corporate profile's lodgements", Status: NotAssessed, Register: true, Link: profileLink,
			Reason: "No sg-acra version is in force on " + today.Format("2 Jan 2006") + ": activate the pack in Settings › Statutory packs"}), nil
	}
	workdays := pack.WorkdaysOf(cal)
	for _, ev := range events {
		p, due, err := pack.AcraDue(acra, acraRule[ev.obligation], ev.on, workdays)
		it := Item{Key: ev.obligation + ":" + ev.id + ":" + ev.on.Format(time.DateOnly), Obligation: ev.obligation, Area: pack.AreaCorporate,
			Title: ev.title, Anchor: ev.on, Why: p.Description, Source: strings.Join(p.Sources, " "), Register: true, Link: profileLink, Pack: acra.ID}
		switch {
		case errors.Is(err, pack.ErrNoRule):
			continue // before the pack's first day: no rule gives it a day
		case errors.Is(err, pack.ErrNoHolidays):
			it.Status, it.Reason = NotAssessed, cal.ID+" holds no public holidays for the year its day falls in: a new version adds them"
		case err != nil:
			return nil, err
		default:
			it.Due, it.Status = due, statusOn(due, today)
		}
		out = append(out, it)
	}
	return out, nil
}

// profileLink is where the corporate profile's records are kept and their lodgements recorded.
const profileLink = "/compliance/corporate-profile"

var roleWords = map[string]string{corporate.RoleDirector: "director", corporate.RoleSecretary: "secretary", corporate.RoleCEO: "CEO",
	corporate.RoleAuditor: "auditor"}
