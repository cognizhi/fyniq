//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

const sampleReport = `{"schema":"fyniq-report@1","rows":[{"id":"rev","label":"Revenue","kind":"group","group":"Revenue"}],"columns":[{"key":"current"}],"filters":{}}`

// A saved report and its schedule (FR-RPT-04, FR-RPT-07; p2-reporting-planning): the API saves a definition at version 1 and
// changes it only as its next version, a live name is used once, nothing is deleted, only archived; the worker alone moves a schedule
// on and records its run, once a day; and another organisation sees none of it.
// req: FR-RPT-04
// req: FR-RPT-07
// asvs: V4.2.1
func TestAReportDefinitionAndItsScheduleKeepTheirRulesInTheDatabase(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	books := porttest.NewBooks(t, api, "Report builder Pte Ltd")
	other := porttest.NewBooks(t, api, "Another report Pte Ltd")
	owner := uuid.New()
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	by := "user:" + owner.String()
	n := storage.ReportDefinition{Name: "Revenue by group", Definition: json.RawMessage(sampleReport), Visibility: storage.ReportShared, OwnerID: owner,
		CreatedBy: by, CreatedAt: at, UpdatedBy: by, UpdatedAt: at}

	// The worker saves no definition; a definition that is no fyniq-report@1 object is refused by the database.
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ReportDefinitions().Create(ctx, n)
		return err
	}); err == nil {
		t.Fatal("the worker saved a report definition")
	}
	bad := n
	bad.Definition = json.RawMessage(`{"schema":"other","rows":[],"columns":[]}`)
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ReportDefinitions().Create(ctx, bad)
		return err
	}); err == nil {
		t.Fatal("a definition with another schema was saved")
	}
	var d storage.ReportDefinition
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		d, err = u.ReportDefinitions().Create(ctx, n)
		return err
	}); err != nil || d.Version != 1 || d.Visibility != storage.ReportShared || d.OwnerID != owner {
		t.Fatalf("the definition: %+v, %v", d, err)
	}
	// A live name is used once, in any case; an archived one frees it.
	twice := n
	twice.Name = "  REVENUE by group "
	err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ReportDefinitions().Create(ctx, twice)
		return err
	})
	var se *storage.Error
	if !errors.As(err, &se) || se.Code != storage.CodeReportName {
		t.Fatalf("a second live definition with the name: %v", err)
	}
	// A change is the next version; one made over a stale version is refused.
	change := d
	change.Name, change.UpdatedAt = "Revenue by group, revised", at.Add(time.Hour)
	var v2 storage.ReportDefinition
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		v2, err = u.ReportDefinitions().Replace(ctx, change, d.Version)
		return err
	}); err != nil || v2.Version != 2 || v2.Name != change.Name {
		t.Fatalf("the change: %+v, %v", v2, err)
	}
	err = api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ReportDefinitions().Replace(ctx, change, d.Version)
		return err
	})
	if !errors.As(err, &se) || se.Code != storage.CodeVersion {
		t.Fatalf("a change over a stale version: %v", err)
	}
	// The owner never changes, and nothing is deleted: the guard holds even for a statement that gets past the grants.
	su := conn(t, "fyniq")
	if _, err := su.Exec(ctx, `SET app.org_id = '`+books.Org.String()+`'`); err != nil {
		t.Fatal(err)
	}
	if _, err := su.Exec(ctx, `DELETE FROM fyniq.report_definition WHERE id = $1`, d.ID); err == nil {
		t.Error("a report definition was deleted")
	}
	if _, err := su.Exec(ctx, `UPDATE fyniq.report_definition SET owner_id = $2, version = version + 1 WHERE id = $1`, d.ID, uuid.New()); err == nil {
		t.Error("a definition's owner was changed")
	}

	// A schedule: weekly needs a weekday and no day of the month; the worker moves it on and records the run, once a day.
	weekday := 1
	s := storage.ReportSchedule{DefinitionID: d.ID, Cadence: storage.CadenceWeekly, Weekday: &weekday, PeriodRule: storage.PeriodLastClosed, Channel: storage.ChannelEmail,
		Recipients: []uuid.UUID{owner}, Active: true, NextRunOn: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), CreatedBy: by, CreatedAt: at, UpdatedAt: at}
	dom := 3
	skewed := s
	skewed.DayOfMonth = &dom
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ReportSchedules().Create(ctx, skewed)
		return err
	}); err == nil {
		t.Fatal("a weekly schedule with a day of the month was saved")
	}
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ReportSchedules().Create(ctx, s)
		return err
	}); err == nil {
		t.Fatal("the worker saved a schedule")
	}
	var saved storage.ReportSchedule
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		saved, err = u.ReportSchedules().Create(ctx, s)
		return err
	}); err != nil || saved.Version != 1 || saved.Weekday == nil || *saved.Weekday != 1 {
		t.Fatalf("the schedule: %+v, %v", saved, err)
	}
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	run := storage.ReportScheduleRun{ScheduleID: saved.ID, RanOn: day, PeriodLabel: "2026-09", LedgerRevision: 42, Sent: 1, Skipped: json.RawMessage(`[]`), RanAt: day.Add(8 * time.Hour)}
	next := day.AddDate(0, 0, 7)
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ReportSchedules().Ran(ctx, run, next, saved.Version)
		return err
	}); err == nil {
		t.Fatal("the API recorded a run")
	}
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		due, err := u.ReportSchedules().Due(ctx, day)
		if err != nil || len(due) != 1 || due[0].ID != saved.ID {
			return errors.Join(err, errors.New("the schedule was not due"))
		}
		moved, err := u.ReportSchedules().Ran(ctx, run, next, due[0].Version)
		if err != nil || moved.LastRunOn == nil || !moved.NextRunOn.Equal(next) || moved.Version != 2 {
			return errors.Join(err, errors.New("the schedule did not move on"))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A second run for the same day is refused; a recorded run is never changed.
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ReportSchedules().Ran(ctx, run, next, 2)
		return err
	}); err == nil {
		t.Fatal("a second run of the same day was recorded")
	}
	if _, err := su.Exec(ctx, `UPDATE fyniq.report_schedule_run SET sent = 9 WHERE schedule_id = $1`, saved.ID); err == nil {
		t.Error("a recorded run was changed")
	}
	// The worker changes nothing a person set: not the recipients, not the cadence.
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ReportSchedules().Replace(ctx, saved, 2)
		return err
	}); err == nil {
		t.Fatal("the worker changed a schedule's recipients")
	}
	// An archived definition leaves the due list, and its name is free again.
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ReportDefinitions().Archive(ctx, d.ID, by, at.Add(2*time.Hour), v2.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		due, err := u.ReportSchedules().Due(ctx, next.AddDate(1, 0, 0))
		if err == nil && len(due) != 0 {
			err = errors.New("an archived definition's schedule is still due")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ReportDefinitions().Create(ctx, twice)
		return err
	}); err != nil {
		t.Fatalf("the archived name was not free: %v", err)
	}
	// Another organisation sees none of it.
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.ReportDefinitions().Get(ctx, d.ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation read the definition: %v", err)
		}
		if _, err := u.ReportSchedules().Get(ctx, saved.ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation read the schedule: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
