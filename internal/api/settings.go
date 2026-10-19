package api

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ organisation settings (FR-ORG-04, FR-ORG-06…08)

// SettingChange is one recorded change to a setting.
type SettingChange struct {
	Value         string `json:"value" doc:"The value from its date." example:"45"`
	EffectiveFrom string `json:"effective_from" format:"date" doc:"The day it takes effect." example:"2026-11-01"`
	Version       int    `json:"version" doc:"The setting's version this change made." example:"2"`
	RecordedAt    string `json:"recorded_at" format:"date-time" doc:"When it was recorded." example:"2026-10-20T09:30:00Z"`
}

// Setting is one organisation setting with its value on a day.
type Setting struct {
	Key                 string          `json:"key" doc:"The setting's key." example:"sales.payment_terms_days"`
	Group               string          `json:"group" doc:"Where settings show it." example:"Defaults"`
	Label               string          `json:"label" doc:"Its name." example:"Payment terms for invoices"`
	Kind                string          `json:"kind" enum:"switch,days,years,amount_sgd,percent,text" doc:"What it holds: switch is \"true\" or \"false\"; days and years whole numbers; amount_sgd Singapore dollars with cents; percent at most two decimals; text a fixed text." example:"days"`
	Value               string          `json:"value" doc:"Its value on the day asked for, as a string." example:"30"`
	EffectiveFrom       string          `json:"effective_from,omitempty" format:"date" doc:"The day that value took effect; absent while it has the value it shipped with." example:"2026-11-01"`
	RecommendedValue    string          `json:"recommended_value" doc:"The value it ships with (FR-ORG-08)." example:"30"`
	Status              string          `json:"status" enum:"recommended,changed" doc:"recommended while it holds the recommended value; changed otherwise." example:"recommended"`
	Reason              string          `json:"reason" doc:"Why the recommended value." example:"An invoice to a customer with no terms of its own falls due 30 days after its date"`
	Guide               string          `json:"guide" doc:"The guide section that explains it." example:"/help/organisation-settings#payment-terms"`
	Requirement         string          `json:"requirement" doc:"The PRD requirement it implements." example:"FR-ORG-04"`
	NeedsSecondApprover bool            `json:"needs_second_approver" doc:"Under dual control (FM-1): a change waits for another member who holds the approval right to approve it (rev 0.36)." example:"false"`
	SetByPlatform       bool            `json:"set_by_platform" doc:"Set by the platform: shown, never changed (FR-ORG-07)." example:"false"`
	LengthenOnly        bool            `json:"lengthen_only" doc:"A change may only lengthen it." example:"false"`
	Min                 string          `json:"min,omitempty" doc:"The smallest value a number may take." example:"0"`
	Max                 string          `json:"max,omitempty" doc:"The largest value a number may take." example:"365"`
	Scheduled           []SettingChange `json:"scheduled" doc:"Changes recorded to take effect after that day, by date."`
	Changes             []SettingChange `json:"changes,omitempty" doc:"Every recorded change, by version; only when one setting is read or changed."`
	Version             int             `json:"version" doc:"The setting's version: its latest change's, 0 before any. It also makes the ETag." example:"0"`
}

// SettingUpdate changes a setting: a value or a reset, from a day.
type SettingUpdate struct {
	Value         *string `json:"value,omitempty" maxLength:"200" doc:"The new value, as its kind writes it; or send reset." example:"45"`
	Reset         bool    `json:"reset,omitempty" doc:"true records the recommended value as a new change, instead of a value." example:"false"`
	EffectiveFrom string  `json:"effective_from,omitempty" format:"date" doc:"The day it takes effect: today, in the organisation's time zone, or later; today when left out." example:"2026-11-01"`
}

// SettingPage is a page of settings, by key.
type SettingPage struct {
	On         string    `json:"on" format:"date" doc:"The day the values are for." example:"2026-10-20"`
	Items      []Setting `json:"items" doc:"The settings on this page."`
	NextCursor string    `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6ImZlYXR1cmVzLnBheXJvbGwifQ"`
}

// SettingPath names a setting.
type SettingPath struct {
	Key string `path:"key" maxLength:"60" doc:"The setting's key." example:"sales.payment_terms_days"`
}

// OnQuery picks the day settings are read for.
type OnQuery struct {
	On string `query:"on" format:"date" doc:"The day to read the values for; today, in the organisation's time zone, when left out." example:"2026-10-20"`
}

type settingListInput struct {
	PageQuery
	OnQuery
}

type settingGetInput struct {
	SettingPath
	OnQuery
}

type settingUpdateInput struct {
	SettingPath
	IfMatchHeader
	PreferHeader
	Body SettingUpdate
}

type settingListOutput struct{ Body SettingPage }

type settingReadOutput struct {
	ETag string `header:"ETag" doc:"The setting's version, for If-Match; \"0\" before any change."`
	Body Setting
}

type settingWriteOutput struct {
	Status  int
	ETag    string `header:"ETag" doc:"The setting's version, for If-Match; absent when the change waits for approval."`
	Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body    any    // a Setting, or the Approval it waits for (202)
}

// SettingCodes are a setting's rules (internal/domain/settings).
var SettingCodes = []string{settings.CodeValue, settings.CodeBackdated, settings.CodeShortened, settings.CodeFixed,
	settings.CodeValueOrReset}

// recordSetting records value as key's next change from effectiveFrom (a date), after checking the setting's
// rules against today; the approval engine runs it when it executes an approved change (rev 0.36).
func (a *API) recordSetting(ctx context.Context, u storage.UnitOfWork, key, value, effectiveFrom string) (Setting, error) {
	d, err := lookupSetting(key)
	if err != nil {
		return Setting{}, err
	}
	from, err := time.Parse(time.DateOnly, effectiveFrom)
	if err != nil {
		return Setting{}, err
	}
	changes, err := u.Settings().Changes(ctx, d.Key)
	if err != nil {
		return Setting{}, err
	}
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return Setting{}, err
	}
	var bad []FieldProblem
	for _, x := range settings.Check(d, value, from, today, changes) {
		bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	if len(bad) > 0 {
		return Setting{}, invalid(bad)
	}
	c, err := u.Settings().Record(ctx, d.Key, settings.On(d, changes, from).Value, value, from, latest(d, changes)+1)
	if err != nil {
		return Setting{}, err
	}
	return settingOf(d, append(changes, c), today, true), nil
}

// orgToday is the organisation's date now, in its time zone, as a date.
func orgToday(ctx context.Context, a *API, u storage.UnitOfWork) (time.Time, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return time.Time{}, err
	}
	loc, err := time.LoadLocation(o.Profile.TimeZone)
	if err != nil || o.Profile.TimeZone == "" {
		loc = time.UTC
	}
	y, m, d := a.deps.Clock.Now().In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), nil
}

// readDay is the day a read is for: on, or the organisation's today.
func readDay(ctx context.Context, a *API, u storage.UnitOfWork, on string) (time.Time, error) {
	if on == "" {
		return orgToday(ctx, a, u)
	}
	d, err := time.Parse(time.DateOnly, on)
	if err != nil {
		return d, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
	}
	return d, nil
}

func changeOf(c storage.SettingChange) SettingChange {
	return SettingChange{Value: c.Value, EffectiveFrom: dateOf(c.EffectiveFrom), Version: c.Version, RecordedAt: c.RecordedAt.UTC().Format(time.RFC3339)}
}

// latest is the version of d's latest change, 0 before any.
func latest(d settings.Definition, changes []storage.SettingChange) int {
	v := 0
	for _, c := range changes {
		if c.Key == d.Key && c.Version > v {
			v = c.Version
		}
	}
	return v
}

// settingOf is d on day, from its changes; all adds every change.
func settingOf(d settings.Definition, changes []storage.SettingChange, day time.Time, all bool) Setting {
	v := settings.On(d, changes, day)
	out := Setting{Key: d.Key, Group: d.Group, Label: d.Label, Kind: string(d.Kind), Value: v.Value, RecommendedValue: d.Recommended,
		Status: "recommended", Reason: d.Reason, Guide: d.Guide, Requirement: d.Requirement, NeedsSecondApprover: d.DualControl,
		SetByPlatform: d.Fixed, LengthenOnly: d.LengthenOnly, Min: d.Min, Max: d.Max, Scheduled: []SettingChange{}, Version: latest(d, changes)}
	if v.Value != d.Recommended {
		out.Status = "changed"
	}
	if v.EffectiveFrom != nil {
		out.EffectiveFrom = dateOf(*v.EffectiveFrom)
	}
	if all {
		out.Changes = []SettingChange{}
	}
	for _, c := range changes {
		if c.Key != d.Key {
			continue
		}
		if c.EffectiveFrom.After(day) {
			out.Scheduled = append(out.Scheduled, changeOf(c))
		}
		if all {
			out.Changes = append(out.Changes, changeOf(c))
		}
	}
	return out
}

// lookupSetting is the setting with key, or the problem that there is none.
func lookupSetting(key string) (settings.Definition, error) {
	d, ok := settings.Lookup(key)
	if !ok {
		return d, problem(CodeNotFound, "No setting has the key "+key+".", "List the settings to see their keys")
	}
	return d, nil
}

func registerSettings(a *API) {
	register(a, authz.ListSettings, huma.Operation{
		OperationID: "listSettings", Method: http.MethodGet, Path: "/v1/organisation/settings", Tags: []string{"Organisation"},
		Summary: "List the organisation settings",
		Description: "Every organisation setting by key, with its value on a day (today when on is left out), its recommended value and " +
			"whether it holds it, why, its guide section, whether it needs a second approver or is set by the platform, and the changes " +
			"scheduled after that day (FR-ORG-04, FR-ORG-06, FR-ORG-07, FR-ORG-08). Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *settingListInput) (*settingListOutput, error) {
		p, _ := authn.From(ctx)
		var all []Setting
		var day time.Time
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			if day, err = readDay(ctx, a, u, in.On); err != nil {
				return err
			}
			changes, err := u.Settings().Changes(ctx, "")
			for _, d := range settings.Catalogue {
				all = append(all, settingOf(d, changes, day, false))
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(s Setting) string { return s.Key }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &settingListOutput{Body: SettingPage{On: dateOf(day), Items: items, NextCursor: next}}, nil
	})

	register(a, authz.GetSetting, huma.Operation{
		OperationID: "getSetting", Method: http.MethodGet, Path: "/v1/organisation/settings/{key}", Tags: []string{"Organisation"},
		Summary:     "Get an organisation setting",
		Description: "One setting with its value on a day (today when on is left out), every recorded change and its ETag (FR-ORG-08). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *settingGetInput) (*settingReadOutput, error) {
		d, err := lookupSetting(in.Key)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out settingReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			day, err := readDay(ctx, a, u, in.On)
			if err != nil {
				return err
			}
			changes, err := u.Settings().Changes(ctx, d.Key)
			out.Body = settingOf(d, changes, day, true)
			out.ETag = etag(out.Body.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.UpdateSetting, pendsApproval(a, huma.Operation{
		OperationID: "updateSetting", Method: http.MethodPatch, Path: "/v1/organisation/settings/{key}", Tags: []string{"Organisation"},
		Summary: "Change an organisation setting",
		Description: "Records a new value for a setting, or with reset its recommended value, from a day: today in the organisation's time " +
			"zone or later, never earlier (FR-ORG-08). Earlier values are kept with their dates. A setting set by the platform never " +
			"changes. One under dual control (FM-1: thresholds, retention, AI) is not changed at once: the answer is 202 with an " +
			"approval request, which another member who holds the approval right (an Owner) approves; the change is made then, if " +
			"nothing it depends on changed (rev 0.36). Asking needs a person signed in who passed step-up in the last 5 minutes. Side " +
			"effects: the setting's next version and its audit event, with the value before and after; or the approval request and its " +
			"audit event. Needs If-Match with its ETag (\"0\" before any change). Prefer: return=preview checks it and saves nothing.",
	}, reflect.TypeFor[Setting]()), append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, storage.CodeVersion,
		storage.CodeRetry, storage.CodeLockTimeout, storage.CodeApproval, CodeSessionRequired, CodeStepUpRequired, approval.CodePending},
		SettingCodes...),
		func(ctx context.Context, in *settingUpdateInput) (*settingWriteOutput, error) {
			d, err := lookupSetting(in.Key)
			if err != nil {
				return nil, err
			}
			if x := settings.Gate(d); x != nil {
				return nil, problem(x.Code, x.Message+".", x.Fix, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (any, error) {
				changes, err := u.Settings().Changes(ctx, d.Key)
				if err != nil {
					return nil, err
				}
				version := latest(d, changes)
				if err := checkIfMatch(in.IfMatch, etag(version)); err != nil {
					return nil, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return nil, err
				}
				b := in.Body
				var bad []FieldProblem
				from := today
				if b.EffectiveFrom != "" {
					if p := parseDay("body.effective_from", b.EffectiveFrom, &bad); p != nil {
						from = *p
					}
				}
				value := d.Recommended
				switch {
				case b.Reset == (b.Value != nil):
					bad = append(bad, FieldProblem{Code: settings.CodeValueOrReset, Field: "body.value", Message: "send a value or reset, not both or neither",
						Fix: "Send the new value, or reset true to go back to the recommended value"})
				case b.Value != nil:
					v, x := settings.Normalise(d, strings.TrimSpace(*b.Value))
					if x != nil {
						bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
					}
					value = v
				}
				if len(bad) == 0 {
					for _, x := range settings.Check(d, value, from, today, changes) {
						bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
					}
				}
				if len(bad) > 0 {
					return nil, invalid(bad)
				}
				if !d.DualControl {
					return a.recordSetting(ctx, u, d.Key, value, dateOf(from))
				}
				was := settings.On(d, changes, from).Value
				c := pendingChange{ObjectType: approval.ObjectSetting, ObjectKey: d.Key, Operation: approval.OpSettingChange,
					Summary: fmt.Sprintf("Change %s from %s to %s from %s", d.Label, was, value, dateOf(from)),
					Execute: map[string]any{"value": value, "effective_from": dateOf(from)},
					Diff:    []ApprovalDiff{{Field: d.Key, Before: was, After: value}, {Field: "effective_from", After: dateOf(from)}}}
				if d.Kind == settings.AmountSGD {
					c.Amounts = []approval.Amount{{Amount: value, Currency: "SGD"}}
				}
				return a.requestApproval(ctx, u, c)
			})
			if err != nil {
				return nil, err
			}
			out := &settingWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
			if s, ok := w.Body.(Setting); ok && !w.Previewed {
				out.ETag = etag(s.Version)
			}
			return out, nil
		})
}
