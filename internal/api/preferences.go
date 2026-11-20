package api

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/preference"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The codes of the preferences: a person's own, presentation only (FR-UX-23).
var preferenceCodes = []string{preference.CodeValue, preference.CodeKeys, preference.CodeReset}

// CodePreferenceNoUser refuses saving preferences for a caller that is not a person who is a member.
const CodePreferenceNoUser = "PRF-04"

// CodePinnedBasic refuses Expert mode to a person whose roles an Administrator pinned to Basic (FR-UX-28).
const CodePinnedBasic = "PRF-05"

// MyPreferences are the caller's own preferences in this organisation, the same on every device (FR-UX-23, FR-UX-08,
// FR-UX-06, FR-UX-38, NFR-L10N-01). They are presentation only: they never change what the caller may do, what the API
// checks or what is saved. A choice never made is absent: the organisation's starting mode for the caller's roles, or
// the standard choice, applies.
type MyPreferences struct {
	Mode           string            `json:"mode,omitempty" enum:"basic,expert" doc:"The experience mode the caller chose; absent until they choose, when default_mode applies." example:"expert"`
	PinnedBasic    bool              `json:"pinned_basic,omitempty" doc:"true (absent otherwise) when an Administrator pinned every role the caller holds to Basic mode (FR-UX-28): they work in Basic, do not see the Basic | Expert switch, and a save of mode expert is refused. A caller with a role that is not pinned is not pinned." example:"false"`
	DefaultMode    string            `json:"default_mode" enum:"basic,expert" doc:"The mode in force while the caller has not chosen: from the organisation's starting mode for each of their roles, the Owner's winning when they hold it (FR-UX-27)." example:"basic"`
	LedgerView     string            `json:"ledger_view,omitempty" enum:"plain,accountant" doc:"Plain view or Accountant view where a screen offers both, in Expert mode (FR-UX-08); absent: plain." example:"plain"`
	Theme          string            `json:"theme,omitempty" enum:"light,dark,system" doc:"Light, dark or the device's setting (FR-UX-38); absent: system." example:"system"`
	Contrast       string            `json:"contrast,omitempty" enum:"standard,more,system" doc:"Standard or more contrast, or the device's setting; absent: system." example:"system"`
	Density        string            `json:"density,omitempty" enum:"comfortable,compact" doc:"The row height of lists and tables; absent: comfortable." example:"comfortable"`
	Motion         string            `json:"motion,omitempty" enum:"on,reduce" doc:"Whether transitions run; the organisation's switch and the device's reduced-motion setting always win; absent: on." example:"on"`
	ShortcutsOn    *bool             `json:"shortcuts_on,omitempty" doc:"Whether the single-key shortcuts are on (WCAG 2.2 SC 2.1.4); absent: on." example:"true"`
	ShortcutKeys   map[string]string `json:"shortcut_keys,omitempty" doc:"The keys the caller chose for a shortcut, by its name on the shortcut sheet; a shortcut not listed keeps its usual keys (FR-UX-06)."`
	NumberFormat   string            `json:"number_format,omitempty" enum:"comma_dot,dot_comma,space_comma,space_dot" doc:"How amounts and counts are written on screen: 1,234.56 (comma_dot), 1.234,56 (dot_comma), 1 234,56 (space_comma) or 1 234.56 (space_dot). Amounts stay exact decimal strings everywhere else (NFR-L10N-01); absent: comma_dot." example:"comma_dot"`
	DateFormat     string            `json:"date_format,omitempty" enum:"d_mmm_yyyy,dd_mm_yyyy,mm_dd_yyyy,iso" doc:"How dates are written on screen: 12 Oct 2026 (d_mmm_yyyy), 12/10/2026 (dd_mm_yyyy), 10/12/2026 (mm_dd_yyyy) or 2026-10-12 (iso), in the organisation's time zone (NFR-L10N-01); absent: d_mmm_yyyy." example:"d_mmm_yyyy"`
	NegativeFormat string            `json:"negative_format,omitempty" enum:"minus,parentheses" doc:"How negative amounts are written on screen: −1,234.56 (minus) or (1,234.56) (parentheses); the sign or the brackets always show, so colour is never the only cue; absent: minus." example:"minus"`
	SetAt          *time.Time        `json:"set_at,omitempty" doc:"When the caller last saved their preferences, if ever." example:"2026-11-20T09:30:00Z"`
	ETag           string            `json:"etag" doc:"The preferences' version, for If-Match." example:"\"9f1c2b7a\""`
}

// PreferenceChange is what a save sets and takes back; whatever it does not name stays as it is.
type PreferenceChange struct {
	Mode           string            `json:"mode,omitempty" enum:"basic,expert" doc:"Set the experience mode." example:"expert"`
	LedgerView     string            `json:"ledger_view,omitempty" enum:"plain,accountant" doc:"Set the Plain or Accountant view." example:"accountant"`
	Theme          string            `json:"theme,omitempty" enum:"light,dark,system" doc:"Set the theme." example:"dark"`
	Contrast       string            `json:"contrast,omitempty" enum:"standard,more,system" doc:"Set the contrast." example:"more"`
	Density        string            `json:"density,omitempty" enum:"comfortable,compact" doc:"Set the density." example:"compact"`
	Motion         string            `json:"motion,omitempty" enum:"on,reduce" doc:"Set the motion." example:"reduce"`
	ShortcutsOn    *bool             `json:"shortcuts_on,omitempty" doc:"Turn the single-key shortcuts on or off." example:"false"`
	ShortcutKeys   map[string]string `json:"shortcut_keys,omitempty" doc:"Replace the keys the caller chose, as a whole: 1 to 12 printable characters each, at most 64 shortcuts. Send an empty object to clear them."`
	NumberFormat   string            `json:"number_format,omitempty" enum:"comma_dot,dot_comma,space_comma,space_dot" doc:"Set the number format." example:"dot_comma"`
	DateFormat     string            `json:"date_format,omitempty" enum:"d_mmm_yyyy,dd_mm_yyyy,mm_dd_yyyy,iso" doc:"Set the date format." example:"iso"`
	NegativeFormat string            `json:"negative_format,omitempty" enum:"minus,parentheses" doc:"Set how negative amounts are written." example:"parentheses"`
	Reset          []string          `json:"reset,omitempty" maxItems:"11" doc:"The choices to take back to their defaults: mode, ledger_view, theme, contrast, density, motion, shortcuts_on, shortcut_keys, number_format, date_format or negative_format." example:"[\"theme\"]"`
}

// TransformSchema implements huma.SchemaTransformer: an example for each item of reset (NFR-API-01).
func (PreferenceChange) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"reset": "theme"})
	return s
}

type setMyPreferencesInput struct {
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body PreferenceChange
}

type myPreferencesOutput struct {
	ETag string `header:"ETag" doc:"The preferences' version, for If-Match."`
	Body MyPreferences
}

type myPreferencesWriteOutput struct {
	ETag     string `header:"ETag" doc:"The preferences' version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     MyPreferences
}

func preferencesETag(s *storage.SavedPreferences) string {
	if s == nil {
		return etag("none")
	}
	return etag(s.SetAt.UnixMicro())
}

// startingModes reads the organisation's starting mode for each role on the organisation's day (FR-UX-27).
func startingModes(ctx context.Context, a *API, u storage.UnitOfWork) (func(role string) string, error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return nil, err
	}
	changes, err := u.Settings().Changes(ctx, "")
	if err != nil {
		return nil, err
	}
	return func(role string) string {
		d, ok := settings.Lookup(preference.DefaultModeKey(role))
		if !ok {
			return ""
		}
		return settings.On(d, changes, today).Value
	}, nil
}

// pinnedBasic is whether every role of a person is pinned to Basic mode by the organisation (FR-UX-28). The line manager is a relationship, not a role (§6.2).
func pinnedBasic(ctx context.Context, a *API, u storage.UnitOfWork, roles []string) (bool, error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return false, err
	}
	changes, err := u.Settings().Changes(ctx, "")
	if err != nil {
		return false, err
	}
	held := slices.DeleteFunc(slices.Clone(roles), func(r string) bool { return r == string(authz.LineManager) })
	return preference.PinnedBasic(held, func(role string) bool {
		d, ok := settings.Lookup(preference.PinnedBasicKey(role))
		return ok && settings.On(d, changes, today).Value == "true"
	}), nil
}

// pin shows a pinned person Basic mode: their saved choice, if it was Expert, is not shown, and their starting mode is Basic.
func pin(out MyPreferences, pinned bool) MyPreferences {
	if pinned {
		out.PinnedBasic, out.DefaultMode, out.Mode = true, "basic", "basic"
	}
	return out
}

// myPreferencesOf is the caller's preferences, with the mode they start in.
func myPreferencesOf(ctx context.Context, a *API, u storage.UnitOfWork, user uuid.UUID, roles []string) (MyPreferences, error) {
	out, err := rawPreferencesOf(ctx, a, u, user, roles)
	if err != nil {
		return out, err
	}
	pinned, err := pinnedBasic(ctx, a, u, roles)
	return pin(out, pinned), err
}

func rawPreferencesOf(ctx context.Context, a *API, u storage.UnitOfWork, user uuid.UUID, roles []string) (MyPreferences, error) {
	start, err := startingModes(ctx, a, u)
	if err != nil {
		return MyPreferences{}, err
	}
	out := MyPreferences{DefaultMode: preference.DefaultMode(roles, start), ETag: preferencesETag(nil)}
	if user == uuid.Nil {
		return out, nil
	}
	s, ok, err := u.Preferences().Latest(ctx, user)
	if err != nil || !ok {
		return out, err
	}
	return fill(out, &s), nil
}

func fill(out MyPreferences, s *storage.SavedPreferences) MyPreferences {
	at := s.SetAt.UTC()
	out.Mode, out.LedgerView, out.Theme, out.Contrast, out.Density, out.Motion = s.Mode, s.LedgerView, s.Theme, s.Contrast, s.Density, s.Motion
	out.ShortcutsOn, out.NumberFormat, out.DateFormat, out.NegativeFormat = s.ShortcutsOn, s.NumberFormat, s.DateFormat, s.NegativeForm
	if len(s.ShortcutKeys) > 0 {
		out.ShortcutKeys = s.ShortcutKeys
	}
	out.SetAt, out.ETag = &at, preferencesETag(s)
	return out
}

func registerPreferences(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.GetMyPreferences, huma.Operation{
		OperationID: "getMyPreferences", Method: http.MethodGet, Path: "/v1/preferences", Tags: []string{"Session"},
		Summary: "Get your preferences",
		Description: "The caller's own preferences in this organisation, the same on every device (FR-UX-23): the experience mode, the Plain or " +
			"Accountant view, the theme, contrast, density and motion, the keyboard shortcuts and the number and date formats, with the mode they " +
			"start in while they have not chosen (FR-UX-27). Presentation only: none of it changes what the caller may do. A caller who is not a " +
			"person gets the defaults. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*myPreferencesOutput, error) {
		p, _ := authn.From(ctx)
		var out MyPreferences
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			user := p.UserID
			if p.Class != authn.ClassUser {
				user = uuid.Nil
			}
			var err error
			out, err = myPreferencesOf(ctx, a, u, user, roleStrings(p.Roles))
			return err
		})
		return &myPreferencesOutput{ETag: out.ETag, Body: out}, err
	})

	register(a, authz.SetMyPreferences, huma.Operation{
		OperationID: "setMyPreferences", Method: http.MethodPatch, Path: "/v1/preferences", Tags: []string{"Session"},
		Summary: "Save your preferences",
		Description: "Saves the caller's own preferences (FR-UX-23): the choices it names are set, those in reset go back to their defaults, and " +
			"the rest stay. Presentation only, and the only thing a mode switch writes. Side effects: the new state and its audit event. Needs " +
			"If-Match with the preferences' etag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodePreferenceNoUser, CodePinnedBasic}, append(slices.Clone(preferenceCodes), locks...)...), func(ctx context.Context, in *setMyPreferencesInput) (*myPreferencesWriteOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
			return nil, problem(CodePreferenceNoUser, "Only a person who is a member has preferences of their own.", "Sign in as yourself")
		}
		change := preference.Change{Set: preference.Preferences{Mode: in.Body.Mode, LedgerView: in.Body.LedgerView, Theme: in.Body.Theme, Contrast: in.Body.Contrast,
			Density: in.Body.Density, Motion: in.Body.Motion, ShortcutsOn: in.Body.ShortcutsOn, ShortcutKeys: in.Body.ShortcutKeys,
			NumberFormat: in.Body.NumberFormat, DateFormat: in.Body.DateFormat, NegativeForm: in.Body.NegativeFormat}, Reset: in.Body.Reset}
		if pr := preference.Validate(change); pr != nil {
			return nil, invalid([]FieldProblem{{Code: pr.Code, Field: "body." + pr.Field, Message: pr.Message, Fix: pr.Fix}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (MyPreferences, error) {
			cur, ok, err := u.Preferences().Latest(ctx, p.UserID)
			if err != nil {
				return MyPreferences{}, err
			}
			var current *storage.SavedPreferences
			if ok {
				current = &cur
			}
			pinned, err := pinnedBasic(ctx, a, u, roleStrings(p.Roles))
			if err != nil {
				return MyPreferences{}, err
			}
			if pinned && in.Body.Mode == "expert" { // FR-UX-28: a pinned role works in Basic; the switch is not theirs
				return MyPreferences{}, problem(CodePinnedBasic, "Your role is pinned to Basic mode, so Expert mode is not available to you.",
					"Ask an Administrator to unpin your role in Settings → Organisation if you need Expert mode")
			}
			if err := checkIfMatch(in.IfMatch, preferencesETag(current)); err != nil {
				return MyPreferences{}, err
			}
			saved, err := u.Preferences().Save(ctx, p.UserID, preference.Apply(cur.Preferences, change))
			if err != nil {
				return MyPreferences{}, err
			}
			start, err := startingModes(ctx, a, u)
			if err != nil {
				return MyPreferences{}, err
			}
			return pin(fill(MyPreferences{DefaultMode: preference.DefaultMode(roleStrings(p.Roles), start)}, &saved), pinned), nil
		})
		if err != nil {
			return nil, err
		}
		return &myPreferencesWriteOutput{ETag: w.Body.ETag, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
