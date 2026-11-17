package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/deadlines"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The statuses of a finding (AI-65): ✔ OK, ⚠ please review, ✖ cannot post until fixed, ⏰ due or overdue, and ⚠ Not
// assessed when an input is missing or out of date.
const (
	FindingOK          = "ok"
	FindingReview      = "review"
	FindingBlocked     = "blocked"
	FindingDue         = "due"
	FindingNotAssessed = "not_assessed"
)

// ReportType is the notification of a run that found something new.
const ReportType = "agent.report"

// Finding is one finding of a run (AI-65; design D5): its stable key, its status, what it is, the rule and source it
// applied, its evidence, what to do next and by when, and its open task.
type Finding struct {
	Key      string   `json:"key"`
	Status   string   `json:"status"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	Rule     string   `json:"rule,omitempty"`
	Source   string   `json:"source,omitempty"`
	Pack     string   `json:"pack,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
	Next     string   `json:"next,omitempty"`
	DueOn    string   `json:"due_on,omitempty"`
	Window   string   `json:"window,omitempty"` // 30, 14, 7 or 1 days ahead, or overdue
	Task     string   `json:"task,omitempty"`
}

// open reports whether f needs a person: due, blocked or to review.
func (f Finding) open() bool {
	return f.Status == FindingDue || f.Status == FindingBlocked || f.Status == FindingReview
}

// secretaryChecks are the checks the company secretary's checklist reads from the checks engine (FR-CS-03 in part).
var secretaryChecks = []string{"gst.registration-threshold", "gst.return-due", "equity.acra-register", "equity.controllers-registered",
	"equity.exempt-status", "equity.instruments-converted"}

// checkStatus is a check's status as a finding's.
var checkStatus = map[string]string{"ok": FindingOK, "warning": FindingReview, "error": FindingBlocked, "due": FindingDue,
	"not_assessed": FindingNotAssessed, "failed": FindingNotAssessed}

// ModelOutcome is what the model step gave: its model, its words as written, what it used and how many proposals it
// made.
type ModelOutcome struct {
	Model     string
	Text      string
	Tokens    int
	ToolCalls int
	Tools     []string // the read tools it called, by name (CTL-AI-09)
	Proposals int
}

// ModelStep asks the AI service, through the API, to explain the findings, cross-check what rules cannot and draft
// (design D5 step 5; rev 0.281). Nil leaves a run deterministic.
type ModelStep func(ctx context.Context, org uuid.UUID, run storage.AgentRun, findings []Finding) (ModelOutcome, error)

// Runner is the worker's work of a queued run (RunKind; AI-64, AI-65, AI-66, AI-67; design D5).
type Runner struct {
	Store      storage.Store
	Dispatcher outbox.Dispatcher
	Clock      ports.Clock
	Skills     *Skills
	Model      ModelStep
}

var errSkip = errors.New("the run is skipped")

// Work runs a queued run: early, it holds until its slot; off, paused or with the AI off, it is skipped with why;
// otherwise the deterministic findings, their tasks, the model step if an input changed and the entry uses it, the
// report, the recipients told and the next slot queued. A run that cannot finish fails with why, which the watchdog
// raises (C.2 #36).
func (r Runner) Work(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
	var in RunWork
	if err := json.Unmarshal(row.Payload, &in); err != nil || in.RunID == uuid.Nil {
		return fmt.Errorf("the run's work names no run: %w", err)
	}
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "agent", ID: "company-secretary"}})
	var run storage.AgentRun
	var skill Skill
	var st State
	var why string
	err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if run, err = u.AgentRuns().Get(ctx, in.RunID); err != nil {
			return err
		}
		if run.Status != storage.AgentRunQueued {
			return errSkip // done already
		}
		now := r.Clock.Now()
		if now.Before(run.SlotAt) {
			return outbox.Held{For: run.SlotAt.Sub(now), Reason: "its slot is not due"}
		}
		var ok bool
		if skill, ok = r.Skills.Get(run.Agent); !ok || skill.Version != run.SkillVersion || skill.SHA256 != run.SkillSHA256 {
			why = "the agent's skill changed since the run was queued; its next slot runs the new one"
		} else if st, err = Enabled(ctx, u, skill, now); err != nil {
			return err
		} else if why, err = Skipped(ctx, u, st, run, now); err != nil {
			return err
		}
		if why != "" {
			_, err = u.AgentRuns().Skip(ctx, run.ID, run.Version, why, now)
			return err
		}
		run, err = u.AgentRuns().Start(ctx, run.ID, run.Version, now)
		return err
	})
	switch {
	case errors.Is(err, errSkip) || (err == nil && why != ""):
		return nil
	case err != nil:
		return err
	}
	if err := r.finish(ctx, org, run, skill, st); err != nil {
		reason := "it could not finish: " + err.Error()
		return r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			cur, err := u.AgentRuns().Get(ctx, run.ID)
			if err != nil || cur.Status != storage.AgentRunRunning {
				return err
			}
			_, err = u.AgentRuns().Fail(ctx, cur.ID, cur.Version, cut(reason, 1000), r.Clock.Now())
			return err
		})
	}
	return nil
}

// Skipped is why a run may not run now, or "": as it starts, and again as it asks the model (the API's model step).
func Skipped(ctx context.Context, u storage.UnitOfWork, st State, run storage.AgentRun, now time.Time) (string, error) {
	switch {
	case !st.On:
		return "the agent is off", nil
	case st.Paused:
		return "the agent is paused", nil
	}
	day := dayIn(now)
	def, _ := settings.Lookup(ai.Switch)
	changes, err := u.Settings().Changes(ctx, ai.Switch)
	if err != nil {
		return "", err
	}
	if settings.On(def, changes, day).Value != "true" {
		return "the AI is switched off", nil
	}
	roles, err := u.Members().Roles(ctx, run.ActsFor, now)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return "", err
	}
	if !slices.ContainsFunc(roles, func(a storage.RoleAssignment) bool { return a.Role == "owner" }) {
		return "the person it acts for is no longer an Owner: an Owner turns it on again", nil
	}
	return "", nil
}

// finish runs a started run: the findings first, the model step outside any transaction, then the tasks, the report,
// the recipients told and the next slot, in one unit of work.
func (r Runner) finish(ctx context.Context, org uuid.UUID, run storage.AgentRun, skill Skill, st State) error {
	now := r.Clock.Now()
	var findings []Finding
	var packs map[string]string
	var revision int64
	var last storage.AgentRun
	err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if findings, packs, err = Deterministic(ctx, u, now); err != nil {
			return err
		}
		if revision, err = u.Ledger().Revision(ctx); err != nil {
			return err
		}
		last, err = u.AgentRuns().LastDone(ctx, run.Agent)
		if errors.Is(err, ports.ErrNotFound) {
			err = nil
		}
		return err
	})
	if err != nil {
		return err
	}
	fingerprint := Fingerprint(findings, packs)
	changed := last.Report == nil || last.Report.Fingerprint != fingerprint
	var outcome ModelOutcome
	used := false
	if r.Model != nil && (changed || !onlyIfChanged(run.Trigger)) {
		if outcome, err = r.Model(ctx, org, run, findings); err != nil {
			return fmt.Errorf("the model step: %w", err)
		}
		used = true
	}
	return r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if err := r.tasks(ctx, u, run, st, findings, now); err != nil {
			return err
		}
		body, err := json.Marshal(findings)
		if err != nil {
			return err
		}
		summary := Summary(findings)
		done, err := u.AgentRuns().Finish(ctx, run.ID, run.Version, storage.AgentReport{Fingerprint: fingerprint, LedgerRevision: revision,
			PackVersions: packs, KBSnapshot: "none", Findings: body, Summary: summary, ModelUsed: used, Model: outcome.Model,
			ModelText: cut(outcome.Text, 20000), Tokens: outcome.Tokens, ToolCalls: outcome.ToolCalls, Tools: outcome.Tools}, now)
		if err != nil {
			return err
		}
		if changed {
			if err := r.tell(ctx, u, org, done, st, findings, summary, now); err != nil {
				return err
			}
		}
		return r.next(ctx, u, org, run, skill, st)
	})
}

// onlyIfChanged reports whether a run's trigger asks the model only when an input changed: the daily monitor, and an
// event, whose model drafts only what it needs (the recommended default schedule).
func onlyIfChanged(trigger string) bool {
	return trigger == "schedule:monitor" || strings.HasPrefix(trigger, "event:")
}

// Deterministic are the findings Fyniq makes before any model step (AI-64), and the packs they read, by name: the
// statutory calendar's items due in the next 30 days or overdue; the checks the checklist reads; and, until the
// corporate profile is recorded, its checks Not assessed, naming the inputs they wait for (FR-CS-03).
func Deterministic(ctx context.Context, u storage.UnitOfWork, now time.Time) ([]Finding, map[string]string, error) {
	today := dayIn(now)
	packs := map[string]string{}
	var out []Finding
	cal, err := deadlines.Read(ctx, u, today, today.AddDate(0, 0, 30), today)
	if err != nil {
		return nil, nil, err
	}
	if cal.Pack != "" {
		packs["sg-deadlines"] = cal.Pack
	}
	for _, it := range cal.Items {
		f := Finding{Key: "calendar:" + it.Key, Title: it.Title, Rule: it.Why, Source: it.Source, Pack: it.Pack,
			Evidence: []string{cmpOr(it.Link, "/compliance/calendar")}, Next: "Record it in Compliance › Statutory calendar once it is filed"}
		if it.Pack != "" {
			name, _, _ := strings.Cut(it.Pack, "@")
			packs[name] = it.Pack
		}
		if !it.Due.IsZero() {
			f.DueOn = it.Due.Format(time.DateOnly)
		}
		left := int(it.Due.Sub(today).Hours() / 24)
		switch it.Status {
		case deadlines.Filed, deadlines.NotRequired:
			f.Status, f.Next = FindingOK, ""
		case deadlines.NotAssessed:
			f.Status, f.Detail, f.Next = FindingNotAssessed, it.Reason, it.Reason
		case deadlines.Overdue:
			f.Status, f.Window, f.Detail = FindingDue, "overdue", "It was due on "+it.Due.Format("2 January 2006")+" and nothing is recorded"
		default:
			f.Status, f.Window, f.Detail = FindingDue, window(left), "Due on "+it.Due.Format("2 January 2006")+", in "+strconv.Itoa(left)+" days"
		}
		out = append(out, f)
	}
	states, err := u.Checks().States(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, key := range secretaryChecks {
		f := Finding{Key: "check:" + key, Evidence: []string{"/checks"}}
		i := slices.IndexFunc(states, func(s storage.CheckState) bool { return s.Key == key })
		if i < 0 {
			f.Status, f.Title, f.Detail = FindingNotAssessed, key, "The check has not run yet"
			out = append(out, f)
			continue
		}
		s := states[i]
		f.Status, f.Title, f.Detail, f.Next = cmpOr(checkStatus[s.Status], FindingNotAssessed), cmpOr(s.Detail, key), s.Detail, s.Fix
		if f.Status == FindingNotAssessed && s.Needs != "" {
			f.Detail = "Not assessed: it needs " + s.Needs
		}
		out = append(out, f)
	}
	out = append(out, Finding{Key: "corporate:profile", Status: FindingNotAssessed, Title: "The corporate profile's checks",
		Detail: "The company secretary, the officers, the registered office, the controllers and the ACRA extract are checked " +
			"against a corporate profile, which is not recorded yet",
		Next: "These checks start once the corporate profile can be recorded"})
	return out, packs, nil
}

// window is the deadline window a day count is in (AI-64): 1, 7, 14 or 30 days ahead.
func window(left int) string {
	for _, w := range []int{1, 7, 14, 30} {
		if left <= w {
			return strconv.Itoa(w)
		}
	}
	return ""
}

// Fingerprint is the SHA-256 of a run's inputs (AI-64): each finding's key, status and window, so a deadline crossing
// into a window changes it with no data change, and the packs read.
func Fingerprint(findings []Finding, packs map[string]string) string {
	type input struct{ Key, Status, Window, Due string }
	in := make([]input, len(findings))
	for i, f := range findings {
		in[i] = input{f.Key, f.Status, f.Window, f.DueOn}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Key < in[j].Key })
	b, _ := json.Marshal(map[string]any{"findings": in, "packs": packs})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Summary is a run's summary in plain words (AI-65): what needs a person, the nearest first, and never "all clear"
// while anything is due, blocked, to review or Not assessed.
func Summary(findings []Finding) string {
	var need []Finding
	notAssessed := 0
	for _, f := range findings {
		switch {
		case f.open():
			need = append(need, f)
		case f.Status == FindingNotAssessed:
			notAssessed++
		}
	}
	na := ""
	if notAssessed > 0 {
		na = fmt.Sprintf(" %d %s not assessed yet, so this is not an all clear.", notAssessed, plural(notAssessed, "item is", "items are"))
	}
	if len(need) == 0 {
		if notAssessed == 0 {
			return "Nothing needs you now."
		}
		return "Nothing is due now." + na
	}
	sort.SliceStable(need, func(i, j int) bool {
		return need[i].DueOn != "" && (need[j].DueOn == "" || need[i].DueOn < need[j].DueOn)
	})
	first := need[0].Title
	if need[0].DueOn != "" {
		day, _ := time.Parse(time.DateOnly, need[0].DueOn)
		first += ", due " + day.Format("2 January 2006")
	}
	return fmt.Sprintf("%d %s you; the nearest is %s.%s", len(need), plural(len(need), "thing needs", "things need"), first, na)
}

// tasks keeps one open task per finding that needs a person (AI-65; design D5 step 3): raised or updated through the
// system principal, owned by the recipients' first role, escalated to the Owner within escalate_days; a run's own task
// whose finding no longer needs anyone is cleared, which marks no obligation met.
func (r Runner) tasks(ctx context.Context, u storage.UnitOfWork, run storage.AgentRun, st State, findings []Finding, now time.Time) error {
	owner := cmpOr(strings.Split(st.Value("recipients"), ",")[0], "owner")
	escalate, _ := strconv.Atoi(cmpOr(st.Value("escalate_days"), "7"))
	today := dayIn(now)
	wanted := map[string]bool{}
	for i := range findings {
		f := &findings[i]
		if !f.open() {
			continue
		}
		wanted[f.Key] = true
		var due *time.Time
		escalated := f.Window == "overdue"
		if f.DueOn != "" {
			d, _ := time.Parse(time.DateOnly, f.DueOn)
			due, escalated = &d, escalated || d.Sub(today) <= time.Duration(escalate)*24*time.Hour
		}
		detail := strings.TrimSpace(f.Detail + ". " + f.Next)
		cur, err := u.ComplianceTasks().OpenFor(ctx, f.Key)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			t, err := u.ComplianceTasks().RaiseForRun(ctx, run.ID, storage.NewComplianceTask{FindingKey: f.Key, Title: cut(f.Title, 200),
				Detail: cut(detail, 2000), OwnerRole: owner, DueOn: due, Escalated: escalated, CreatedAt: now})
			if err != nil {
				return err
			}
			f.Task = t.ID.String()
			continue
		case err != nil:
			return err
		}
		f.Task = cur.ID.String()
		if cur.Title != cut(f.Title, 200) || cur.Detail != cut(detail, 2000) || cur.Escalated != escalated || !sameDay(cur.DueOn, due) {
			if _, err := u.ComplianceTasks().Update(ctx, cur.ID, cur.Version, storage.TaskChange{Title: cut(f.Title, 200), Detail: cut(detail, 2000),
				OwnerRole: cur.OwnerRole, OwnerUser: cur.OwnerUser, DueOn: due, Escalated: escalated}); err != nil {
				return err
			}
		}
	}
	open, err := u.ComplianceTasks().List(ctx, storage.ComplianceTaskQuery{Statuses: []string{storage.TaskOpen}})
	if err != nil {
		return err
	}
	for _, t := range open {
		if t.Source == "agent_run" && !wanted[t.FindingKey] {
			if _, err := u.ComplianceTasks().Clear(ctx, t.ID, t.Version, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// tell notifies the recipients of a run that found something new, in the Inbox and, by their own preferences, in the
// daily email, which waits past quiet hours (AI-62): the Owner too when a finding is escalated.
func (r Runner) tell(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, run storage.AgentRun, st State, findings []Finding,
	summary string, now time.Time) error {
	if !slices.Contains(strings.Split(st.Value("channels"), ","), "inbox") && !slices.Contains(strings.Split(st.Value("channels"), ","), "email") {
		return nil
	}
	roles := strings.Split(st.Value("recipients"), ",")
	if slices.ContainsFunc(findings, func(f Finding) bool { return f.Window == "overdue" || f.Window == "1" || f.Window == "7" }) {
		roles = append(roles, "owner")
	}
	members, err := u.Members().List(ctx, now)
	if err != nil {
		return err
	}
	var users []uuid.UUID
	for _, m := range members {
		if m.Status == "active" && slices.ContainsFunc(m.Roles, func(a storage.RoleAssignment) bool { return slices.Contains(roles, a.Role) }) {
			users = append(users, m.UserID)
		}
	}
	if len(users) == 0 {
		return nil
	}
	id := run.ID
	_, err = notify.Emit(ctx, r.Dispatcher, u, org, notify.Event{Type: ReportType, RecordType: "agent_run", RecordID: &id, Title: summary,
		Link: "/compliance/secretary/runs/" + run.ID.String(), At: now, Audience: notify.Audience{Users: users}})
	return err
}

// next queues the slot after this one of a scheduled run's entry, so each run enqueues its next (AI-62).
func (r Runner) next(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, run storage.AgentRun, skill Skill, st State) error {
	entry, ok := strings.CutPrefix(run.Trigger, "schedule:")
	if !ok {
		return nil
	}
	spec, err := st.Entry(entry)
	if err != nil {
		return err
	}
	slot := spec.Next(run.SlotAt)
	if slot.IsZero() || slot.Sub(r.Clock.Now()) > 30*24*time.Hour {
		return nil
	}
	s := Scheduler{Store: r.Store, Dispatcher: r.Dispatcher, Clock: r.Clock, Skills: r.Skills}
	_, err = s.Queue(ctx, u, org, skill, run.ActsFor, run.Trigger, slot, nil)
	return err
}

func dayIn(t time.Time) time.Time {
	y, m, d := t.In(clock.Singapore).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func sameDay(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Format(time.DateOnly) == b.Format(time.DateOnly)
}

func cmpOr(v, d string) string {
	if v != "" {
		return v
	}
	return d
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
