package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the close checklist (FR-GL-07, FM-4)

// ChecklistCodes are the codes of the close checklist.
var ChecklistCodes = []string{closing.CodeTemplate, closing.CodeNoTask, closing.CodeSkipReason, storage.CodeChecklist}

// ChecklistTask is one task of the close checklist template.
type ChecklistTask struct {
	Key       string `json:"key" pattern:"^[a-z0-9][a-z0-9-]{0,39}$" doc:"The task's key: the month's checklist marks it by it." example:"bank"`
	Title     string `json:"title" doc:"What the task is." example:"Bank, card and wallet accounts reconciled"`
	Detail    string `json:"detail,omitempty" doc:"More about it." example:"Each balance agrees with its statement at the month's end"`
	OwnerRole string `json:"owner_role,omitempty" enum:"owner,controller,accountant,bookkeeper,ar_specialist,ap_specialist,payroll_administrator,tax_agent" doc:"The role that usually does it." example:"accountant"`
	DueDays   int    `json:"due_days" minimum:"0" maximum:"60" doc:"Due this many days after the month's end." example:"7"`
}

// ChecklistTemplate is the close checklist template in force.
type ChecklistTemplate struct {
	Version     int             `json:"version" doc:"Its version; 0 for the platform's recommended template, in force until the organisation saves one." example:"2"`
	Recommended bool            `json:"recommended" doc:"true while it is the platform's recommended template." example:"false"`
	Tasks       []ChecklistTask `json:"tasks" nullable:"false" doc:"Its tasks, in order."`
	WrittenBy   *Person         `json:"written_by,omitempty" doc:"Who saved this version; absent for the recommended template."`
	WrittenAt   *time.Time      `json:"written_at,omitempty" doc:"When." example:"2026-10-22T09:00:00Z"`
}

// ChecklistTaskInput is a task as a template's change sends it.
type ChecklistTaskInput struct {
	Key       string `json:"key,omitempty" maxLength:"40" doc:"The task's key: keep it for a task the template has, so its months keep their marks; leave it out for a new task, which gets one from its title." example:"bank"`
	Title     string `json:"title" minLength:"1" maxLength:"200" doc:"What the task is." example:"Bank, card and wallet accounts reconciled"`
	Detail    string `json:"detail,omitempty" maxLength:"500" doc:"More about it." example:"Each balance agrees with its statement at the month's end"`
	OwnerRole string `json:"owner_role,omitempty" enum:"owner,controller,accountant,bookkeeper,ar_specialist,ap_specialist,payroll_administrator,tax_agent" doc:"The role that usually does it." example:"accountant"`
	DueDays   int    `json:"due_days,omitempty" minimum:"0" maximum:"60" doc:"Due this many days after the month's end; 0, the default, at the month's end." example:"7"`
}

// ChecklistTemplateRequest is the template's next version: the whole list, in order.
type ChecklistTemplateRequest struct {
	Tasks []ChecklistTaskInput `json:"tasks" minItems:"1" maxItems:"40" doc:"Every task, in order: a task left out is no longer on the checklist."`
}

// ChecklistItem is one task of a month's checklist and where it stands.
type ChecklistItem struct {
	Key       string     `json:"key" doc:"The task's key." example:"bank"`
	Title     string     `json:"title" doc:"What the task is." example:"Bank, card and wallet accounts reconciled"`
	Detail    string     `json:"detail,omitempty" doc:"More about it." example:"Each balance agrees with its statement at the month's end"`
	OwnerRole string     `json:"owner_role,omitempty" doc:"The role that usually does it." example:"accountant"`
	DueOn     string     `json:"due_on" format:"date" doc:"When it is due: the month's end plus the template's due days." example:"2026-11-07"`
	State     string     `json:"state" enum:"open,done,skipped" doc:"open until it is done, or skipped for the month with a reason; the close checks refuse the close while one is open." example:"done"`
	Note      string     `json:"note,omitempty" doc:"What was done, or why it was skipped." example:"4 statements agreed"`
	By        *Person    `json:"by,omitempty" doc:"Who marked it last."`
	At        *time.Time `json:"at,omitempty" doc:"When." example:"2026-11-06T02:00:00Z"`
}

// PeriodChecklist is a month's close checklist.
type PeriodChecklist struct {
	Period          Period          `json:"period"`
	TemplateVersion int             `json:"template_version" doc:"The template version its tasks come from; 0 for the recommended template." example:"2"`
	Done            int             `json:"done" doc:"How many tasks are done." example:"4"`
	Skipped         int             `json:"skipped" doc:"How many are skipped for the month." example:"1"`
	Open            int             `json:"open" doc:"How many are neither: the close checks refuse the close while any is (Appendix C.2 #58)." example:"1"`
	Tasks           []ChecklistItem `json:"tasks" nullable:"false" doc:"Its tasks, in the template's order."`
}

type checklistTemplateOutput struct {
	ETag string `header:"ETag" doc:"The template's version, for If-Match."`
	Body ChecklistTemplate
}

type replaceChecklistTemplateInput struct {
	IfMatchHeader
	PreferHeader
	Body ChecklistTemplateRequest
}

type replaceChecklistTemplateOutput struct {
	ETag    string `header:"ETag" doc:"The new version, for If-Match."`
	Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body    ChecklistTemplate
}

type periodChecklistOutput struct {
	Body PeriodChecklist
}

// ChecklistMark marks a task of a month's checklist.
type ChecklistMark struct {
	State string `json:"state" enum:"done,skipped,open" doc:"done; skipped for the month, with a reason in note; or open again." example:"done"`
	Note  string `json:"note,omitempty" maxLength:"1000" doc:"What was done; for a skipped task, why it does not apply this month." example:"4 statements agreed"`
}

type markChecklistInput struct {
	PeriodPath
	TaskKey string `path:"task_key" maxLength:"40" doc:"The task's key." example:"bank"`
	KeyHeader
	PreferHeader
	Body ChecklistMark
}

type markChecklistOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     PeriodChecklist
}

func checklistTemplateOf(t closing.Template) ChecklistTemplate {
	out := ChecklistTemplate{Version: t.Version, Recommended: t.Version == 0, Tasks: make([]ChecklistTask, 0, len(t.Tasks)), WrittenBy: personOf(t.WrittenBy)}
	if t.WrittenAt != nil {
		at := t.WrittenAt.UTC()
		out.WrittenAt = &at
	}
	for _, x := range t.Tasks {
		out.Tasks = append(out.Tasks, ChecklistTask{Key: x.Key, Title: x.Title, Detail: x.Detail, OwnerRole: x.OwnerRole, DueDays: x.DueDays})
	}
	return out
}

func checklistTemplateETag(t ChecklistTemplate) string { return etag(t.Version) }

// periodChecklist reads a month's checklist, with its people named.
func (a *API) periodChecklist(ctx context.Context, u storage.UnitOfWork, c closing.Checklist) (PeriodChecklist, error) {
	cal, err := calendar(ctx, u)
	if err != nil {
		return PeriodChecklist{}, err
	}
	out := PeriodChecklist{Period: periodOf(c.Period, cal), TemplateVersion: c.Template.Version, Done: c.Count(storage.ChecklistDone),
		Skipped: c.Count(storage.ChecklistSkipped), Open: c.Count(storage.ChecklistOpen), Tasks: make([]ChecklistItem, 0, len(c.Items))}
	var people []*Person
	for _, it := range c.Items {
		x := ChecklistItem{Key: it.Key, Title: it.Title, Detail: it.Detail, OwnerRole: it.OwnerRole, DueOn: it.Due.Format(time.DateOnly), State: it.State}
		if e := it.Entry; e != nil {
			at := e.WrittenAt.UTC()
			x.Note, x.By, x.At = e.Note, personOf(e.WrittenBy), &at
			people = append(people, x.By)
		}
		out.Tasks = append(out.Tasks, x)
	}
	return out, a.name(ctx, u, people...)
}

func registerChecklist(a *API) {
	register(a, authz.GetCloseChecklistTemplate, huma.Operation{
		OperationID: "getCloseChecklistTemplate", Method: http.MethodGet, Path: "/v1/close-checklist-template", Tags: []string{"Periods"},
		Summary: "Get the close checklist template",
		Description: "The tasks every month's close checklist has (FR-GL-07; PRD-001 rev 0.68, provisional), in order, each with the role " +
			"that usually does it and the days after the month's end it is due by: the organisation's latest version, or the platform's " +
			"recommended template (version 0) while it has saved none. With its ETag. Read-only; no side effects.",
	}, []string{}, func(ctx context.Context, _ *struct{}) (*checklistTemplateOutput, error) {
		p, _ := authn.From(ctx)
		var out checklistTemplateOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			t, err := closing.CurrentTemplate(ctx, u)
			if err != nil {
				return err
			}
			out.Body = checklistTemplateOf(t)
			out.ETag = checklistTemplateETag(out.Body)
			return a.name(ctx, u, out.Body.WrittenBy)
		})
		return &out, err
	})

	register(a, authz.ReplaceCloseChecklistTemplate, huma.Operation{
		OperationID: "replaceCloseChecklistTemplate", Method: http.MethodPut, Path: "/v1/close-checklist-template", Tags: []string{"Periods"},
		Summary: "Change the close checklist template",
		Description: "Saves the close checklist template's next version: every task, in order (FR-GL-07; the founders' decision of 22 Oct " +
			"2026: the Accountant or the Controller changes it). A task keeps its key so the months already marked keep their marks; " +
			"a new task leaves it out and gets one from its title; a task left out is no longer on any open month's checklist. One to " +
			"40 tasks, each with a title of up to 200 characters, a detail of up to 500, an owner among the roles that work on the " +
			"close, and a due day from 0 to 60 after the month's end; every problem is listed at once (CLOSE-11). A version never " +
			"changes. Side effects: the new version and its audit event. Needs If-Match with the template's ETag (\"0\" for the " +
			"recommended template). Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeDuplicate, storage.CodeRetry,
		storage.CodeLockTimeout}, ChecklistCodes...),
		func(ctx context.Context, in *replaceChecklistTemplateInput) (*replaceChecklistTemplateOutput, error) {
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ChecklistTemplate, error) {
				before, err := closing.CurrentTemplate(ctx, u)
				if err != nil {
					return ChecklistTemplate{}, err
				}
				if err := checkIfMatch(in.IfMatch, checklistTemplateETag(checklistTemplateOf(before))); err != nil {
					return ChecklistTemplate{}, err
				}
				tasks := make([]closing.Task, 0, len(in.Body.Tasks))
				for _, x := range in.Body.Tasks {
					tasks = append(tasks, closing.Task{Key: x.Key, Title: x.Title, Detail: x.Detail, OwnerRole: x.OwnerRole, DueDays: x.DueDays})
				}
				t, probs, err := closing.SaveTemplate(ctx, u, tasks)
				if err != nil {
					return ChecklistTemplate{}, err
				}
				if len(probs) > 0 {
					fields := make([]FieldProblem, 0, len(probs))
					for _, p := range probs {
						fields = append(fields, FieldProblem{Code: p.Code, Field: "body." + p.Field, Message: p.Message, Fix: p.Fix})
					}
					return ChecklistTemplate{}, invalid(fields)
				}
				out := checklistTemplateOf(t)
				return out, a.name(ctx, u, out.WrittenBy)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &replaceChecklistTemplateOutput{Applied: applied(w), Body: w.Body}, nil
			}
			return &replaceChecklistTemplateOutput{ETag: checklistTemplateETag(w.Body), Body: w.Body}, nil
		})

	register(a, authz.GetPeriodChecklist, huma.Operation{
		OperationID: "getPeriodChecklist", Method: http.MethodGet, Path: "/v1/periods/{period_id}/checklist", Tags: []string{"Periods"},
		Summary: "Get a month's close checklist",
		Description: "A month's close checklist (FR-GL-07, FM-4; PRD-001 rev 0.68, provisional): the current template's tasks, in order, " +
			"each due the given days after the month's end, with its latest mark: done, skipped for the month with a reason, or open, " +
			"and who marked it when. The close checks refuse the close while a task is open (Appendix C.2 #58). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *PeriodPath) (*periodChecklistOutput, error) {
		id, err := parseID("period_id", in.PeriodID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out periodChecklistOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := closing.ChecklistOf(ctx, u, id)
			if err != nil {
				return err
			}
			out.Body, err = a.periodChecklist(ctx, u, c)
			return err
		})
		return &out, err
	})

	register(a, authz.MarkChecklistTask, huma.Operation{
		OperationID: "markChecklistTask", Method: http.MethodPost, Path: "/v1/periods/{period_id}/checklist/{task_key}", Tags: []string{"Periods"},
		Summary: "Mark a task of a month's close checklist",
		Description: "Marks a task of an open month's close checklist done, skipped for the month with a reason (CLOSE-13), or open again " +
			"(FR-GL-07; PRD-001 rev 0.68, provisional), by the members who run the close checks. Each mark is kept, append-only: the " +
			"latest is the task's state, and the database refuses one in a month that is not open (STORAGE-CLOSE-03). Run the close " +
			"checks again afterwards: they record what they found when they ran. Side effects: the mark and its audit event. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and records nothing.",
	}, append([]string{CodeNotFound, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		storage.CodeRetry, storage.CodeLockTimeout, closing.CodeNotOpen}, ChecklistCodes...),
		func(ctx context.Context, in *markChecklistInput) (*markChecklistOutput, error) {
			id, err := parseID("period_id", in.PeriodID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PeriodChecklist, error) {
				if _, err := closing.MarkTask(ctx, u, id, in.TaskKey, in.Body.State, in.Body.Note); err != nil {
					return PeriodChecklist{}, err
				}
				c, err := closing.ChecklistOf(ctx, u, id)
				if err != nil {
					return PeriodChecklist{}, err
				}
				return a.periodChecklist(ctx, u, c)
			})
			if err != nil {
				return nil, err
			}
			return &markChecklistOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
