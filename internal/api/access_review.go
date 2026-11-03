package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/accessreview"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// ------------------------------------------------------------------ the user access review (FR-CTL-06, SEC-06; rev 0.162)

// AccessReviewConflict is a segregation-of-duties conflict a member's roles hold, and what covers it.
type AccessReviewConflict struct {
	SoD     string     `json:"sod" doc:"The conflict of §6.3." example:"SOD-6"`
	Roles   []RoleName `json:"roles" doc:"The roles that make it."`
	Covered string     `json:"covered" enum:"founder_mode,override,no" doc:"founder_mode: Founder mode allows it with its compensating controls (§6.4); override: the team mode blocks it and an approved override lets this member keep it; no: nothing covers it (C.2 #5)." example:"founder_mode"`
	Reason  string     `json:"override_reason,omitempty" doc:"The override's reason, when one covers it." example:"Two founders; reviewed weekly"`
}

// AccessReviewMember is a member as the access review shows them.
type AccessReviewMember struct {
	MemberID     string                 `json:"member_id" format:"uuid" doc:"The membership's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f01"`
	UserID       string                 `json:"user_id" format:"uuid" doc:"The member's user." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	DisplayName  string                 `json:"display_name" doc:"Their name." example:"Wei Ling Tan"`
	Roles        []MemberRole           `json:"roles" doc:"Their live roles, by role, with an external user's end date."`
	ExpiredRoles []MemberRole           `json:"expired_roles" doc:"Roles that ended at their end date and that nobody ended: they grant nothing. A member who holds only these is an external user past their expiry, still a member (C.2 #6): remove them, or give a new end date."`
	External     bool                   `json:"external" doc:"true when a role they hold, or held until it expired, has an end date: an external user, reviewed each quarter (SEC-06)." example:"false"`
	PastExpiry   bool                   `json:"past_expiry" doc:"true for an external user whose roles have all ended while they are still a member (C.2 #6)." example:"false"`
	JoinedAt     time.Time              `json:"joined_at" doc:"When they joined." example:"2026-10-01T09:00:00Z"`
	LastSignIn   *time.Time             `json:"last_sign_in,omitempty" doc:"Their latest sign-in, by any of their identities; absent if they never signed in." example:"2026-10-30T08:15:00Z"`
	SecondFactor string                 `json:"second_factor" enum:"passkey,sign_in,none" doc:"How their latest session passed its second factor: passkey, a passkey; sign_in, at the sign-in provider (an authenticator app); none, not yet, or never signed in." example:"passkey"`
	Dormant      bool                   `json:"dormant" doc:"true when they have not signed in for 30 days, counting from when they joined if never (FR-CTL-06)." example:"false"`
	Conflicts    []AccessReviewConflict `json:"conflicts" doc:"The conflicts their live roles hold."`
}

// AccessReviewConnector is a connector the organisation installed: a system that reaches its data (AI-34, FR-INT-20).
type AccessReviewConnector struct {
	ID          string      `json:"id" format:"uuid" doc:"The installed connector's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f40"`
	Connector   string      `json:"connector" doc:"The connector's ID in the catalogue." example:"wise"`
	Name        string      `json:"name" doc:"Its name." example:"Wise"`
	State       string      `json:"state" doc:"Whether it is enabled." example:"enabled"`
	Health      string      `json:"health" enum:"ok,failing,unknown" doc:"Its last connection test: ok, failing, or unknown until it is tested." example:"ok"`
	Scopes      []ScopeName `json:"fyniq_scopes" doc:"The Fyniq scopes it was granted."`
	DataClasses []DataClass `json:"data_classes" doc:"The data classes it may send out."`
	InstalledBy string      `json:"installed_by" doc:"Who installed it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	InstalledAt time.Time   `json:"installed_at" doc:"When." example:"2026-10-01T09:00:00Z"`
}

// AccessReviewQuarter is a calendar quarter of the review.
type AccessReviewQuarter struct {
	Quarter string `json:"quarter" format:"date" doc:"Its first day." example:"2026-07-01"`
	Label   string `json:"label" doc:"Its name." example:"Q3 2026"`
	DueBy   string `json:"due_by" format:"date" doc:"The last day its review is on time: 14 days after it ends (C.2 #7)." example:"2026-10-14"`
}

// AccessReviewAttestation is an Owner's attestation of a quarter.
type AccessReviewAttestation struct {
	Quarter        AccessReviewQuarter `json:"quarter" doc:"The quarter attested."`
	AttestedBy     string              `json:"attested_by" doc:"Who attested it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	AttestedByName string              `json:"attested_by_name,omitempty" doc:"Their name, while they are a member." example:"Wei Ling Tan"`
	AttestedAt     time.Time           `json:"attested_at" doc:"When." example:"2026-10-05T10:00:00Z"`
	Note           string              `json:"note,omitempty" doc:"What they noted." example:"Ended the auditor's access."`
	ReportSHA256   string              `json:"report_sha256" doc:"The SHA-256 of the report they attested, as the database recorded it, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
}

// AccessReview is who can do what in the organisation, live, and its quarterly attestations (FR-CTL-06).
type AccessReview struct {
	AsAt         time.Time                 `json:"as_at" doc:"When it was read." example:"2026-10-05T10:00:00Z"`
	TeamMode     string                    `json:"team_mode" enum:"founder,standard,enterprise" doc:"The team mode in force (§6.4)." example:"founder"`
	Members      []AccessReviewMember      `json:"members" doc:"The active members, in the order they joined."`
	Invitations  []MemberInvitation        `json:"invitations" doc:"The pending invitations: who will join, with which roles."`
	Connectors   []AccessReviewConnector   `json:"connectors" doc:"The organisation's installed connectors. OAuth clients join with Connected apps (AI-46)."`
	ToAttest     *AccessReviewQuarter      `json:"to_attest,omitempty" doc:"The latest calendar quarter that has ended, while it is not attested; absent when it is, or no quarter has ended since the organisation began."`
	Overdue      *AccessReviewQuarter      `json:"overdue,omitempty" doc:"A quarter whose review is past its due date with neither its attestation nor a later one (C.2 #7)."`
	Attestations []AccessReviewAttestation `json:"attestations" doc:"The quarters attested, latest first."`
}

type accessReviewOutput struct {
	ETag string `header:"ETag" doc:"The review's version: who holds which roles, the conflicts, the external users, the invitations and the connectors, for If-Match. Sign-ins do not change it."`
	Body AccessReview
}

// AttestAccessReviewRequest attests the latest quarter that has ended.
type AttestAccessReviewRequest struct {
	Quarter string `json:"quarter" format:"date" doc:"The quarter's first day, as to_attest names it: what you attest." example:"2026-07-01"`
	Note    string `json:"note,omitempty" maxLength:"1000" doc:"What you found or changed, for the record." example:"Ended the auditor's access."`
}

type attestAccessReviewInput struct {
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body AttestAccessReviewRequest
}

type accessReviewWriteOutput struct {
	ETag     string `header:"ETag" doc:"The review's version after the command, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     AccessReview
}

type accessReviewFileOutput struct{ Body ReportFile }

func quarterOut(q time.Time) AccessReviewQuarter {
	return AccessReviewQuarter{Quarter: q.Format(time.DateOnly), Label: accessreview.Label(q), DueBy: accessreview.DueBy(q).Format(time.DateOnly)}
}

// accessReviewOf reads the review as it stands, and its ETag.
func (a *API) accessReviewOf(ctx context.Context, u storage.UnitOfWork) (AccessReview, string, error) {
	now := a.deps.Clock.Now()
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return AccessReview{}, "", err
	}
	mode, version, _, err := teamModeOf(ctx, u)
	if err != nil {
		return AccessReview{}, "", err
	}
	overrides, err := u.TeamModes().Overrides(ctx)
	if err != nil {
		return AccessReview{}, "", err
	}
	ms, err := u.Members().List(ctx, now)
	if err != nil {
		return AccessReview{}, "", err
	}
	signIns, err := u.AccessReviews().SignIns(ctx)
	if err != nil {
		return AccessReview{}, "", err
	}
	invs, err := u.MemberInvitations().Pending(ctx, now)
	if err != nil {
		return AccessReview{}, "", err
	}
	installed, err := u.Connectors().Organisation(ctx)
	if err != nil {
		return AccessReview{}, "", err
	}
	attested, err := u.AccessReviews().List(ctx)
	if err != nil {
		return AccessReview{}, "", err
	}
	out := AccessReview{AsAt: now.UTC(), TeamMode: mode, Members: []AccessReviewMember{}, Invitations: []MemberInvitation{},
		Connectors: []AccessReviewConnector{}, Attestations: []AccessReviewAttestation{}}
	names := map[uuid.UUID]string{}
	since := today
	for _, m := range ms {
		names[m.UserID] = m.DisplayName
		if d := m.Since.UTC().Truncate(24 * time.Hour); d.Before(since) {
			since = d
		}
		out.Members = append(out.Members, reviewMemberOf(m, signIns, mode, version, overrides, now))
	}
	for _, i := range invs {
		out.Invitations = append(out.Invitations, invitationOf(i))
	}
	for _, c := range installed {
		if c.Installation || c.State == storage.ConnectorUninstalled {
			continue
		}
		rc := AccessReviewConnector{ID: c.ID.String(), Connector: c.Connector, Name: c.Connector, State: c.State, Health: "unknown",
			Scopes: named(c.Consent.FyniqScopes, asScopeName), DataClasses: named(c.Consent.DataClasses, asDataClass), InstalledBy: c.InstalledBy, InstalledAt: c.InstalledAt.UTC()}
		if a.deps.Connectors.Catalogue != nil {
			if b, ok := a.deps.Connectors.Catalogue.Get(c.Connector); ok {
				rc.Name = b.Manifest.Name
			}
		}
		if c.Test != nil {
			switch {
			case !c.Test.Passed:
				rc.Health = "failing"
			case c.State == storage.ConnectorEnabled:
				rc.Health = "ok"
			}
		}
		out.Connectors = append(out.Connectors, rc)
	}
	var quarters []time.Time
	for _, x := range slices.Backward(attested) {
		quarters = append(quarters, x.Quarter)
		v := AccessReviewAttestation{Quarter: quarterOut(x.Quarter), AttestedBy: x.AttestedBy, AttestedAt: x.AttestedAt.UTC(), Note: x.Note,
			ReportSHA256: hex.EncodeToString(x.ReportHash)}
		if id, err := uuid.Parse(strings.TrimPrefix(x.AttestedBy, "user:")); err == nil {
			v.AttestedByName = names[id]
		}
		out.Attestations = append(out.Attestations, v)
	}
	if q, ok := accessreview.ToAttest(today, since); ok && !accessreview.Covered(q, quarters) {
		tq := quarterOut(q)
		out.ToAttest = &tq
	}
	if q, ok := accessreview.Due(today, since); ok && !accessreview.Covered(q, quarters) {
		dq := quarterOut(q)
		out.Overdue = &dq
	}
	tag, err := accessReviewETag(out)
	return out, tag, err
}

// reviewMemberOf is one member as the review shows them, at now.
func reviewMemberOf(m storage.Member, signIns []storage.MemberSignIn, mode string, version int, overrides []storage.SoDOverride, now time.Time) AccessReviewMember {
	v := AccessReviewMember{MemberID: m.ID.String(), UserID: m.UserID.String(), DisplayName: m.DisplayName, Roles: []MemberRole{},
		ExpiredRoles: []MemberRole{}, JoinedAt: m.Since.UTC(), SecondFactor: "none", Conflicts: []AccessReviewConflict{}}
	for _, r := range m.Roles {
		v.Roles = append(v.Roles, roleOut(r))
		v.External = v.External || r.ExpiresAt != nil
	}
	for _, r := range m.Expired {
		v.ExpiredRoles = append(v.ExpiredRoles, roleOut(r))
		v.External = true
	}
	v.PastExpiry = len(m.Roles) == 0 && len(m.Expired) > 0
	last := m.Since
	if i := slices.IndexFunc(signIns, func(s storage.MemberSignIn) bool { return s.UserID == m.UserID }); i >= 0 {
		at := signIns[i].LastSignIn.UTC()
		v.LastSignIn, v.SecondFactor, last = &at, cmpOr(signIns[i].StepUp, "none"), at
	}
	v.Dormant = now.Sub(last) > accessreview.Dormant
	for _, c := range authz.RoleConflicts(authzRoles(liveRoleNames(m))) {
		rc := AccessReviewConflict{SoD: c.SoD, Covered: "no"}
		for _, r := range c.Roles {
			rc.Roles = append(rc.Roles, RoleName(r))
		}
		switch i := slices.IndexFunc(overrides, func(o storage.SoDOverride) bool {
			return o.TeamModeVersion == version && o.UserID == m.UserID && o.Conflict == c.SoD
		}); {
		case !team.Blocks(mode):
			rc.Covered = "founder_mode"
		case i >= 0:
			rc.Covered, rc.Reason = "override", overrides[i].Reason
		}
		v.Conflicts = append(v.Conflicts, rc)
	}
	return v
}

func liveRoleNames(m storage.Member) []string {
	out := make([]string, len(m.Roles))
	for i, r := range m.Roles {
		out[i] = r.Role
	}
	return out
}

// accessReviewETag is the review's version: its canonical JSON without when it was read and the members' sign-ins,
// which change with every sign-in and step-up, the attester's own included.
func accessReviewETag(r AccessReview) (string, error) {
	r.AsAt = time.Time{}
	r.Members = slices.Clone(r.Members)
	for i := range r.Members {
		r.Members[i].LastSignIn, r.Members[i].SecondFactor, r.Members[i].Dormant = nil, "", false
	}
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:8]) + `"`, nil
}

// accessReviewSheet is the review as a table: a row per member, then a row per pending invitation and connector.
func accessReviewSheet(r AccessReview) sheet.Sheet {
	s := sheet.Sheet{Name: "Access review", Header: []string{"Kind", "Name", "Roles", "Expired roles", "External", "Joined or installed",
		"Last sign-in", "Second factor", "Dormant", "Conflicts", "Health"}}
	yes := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	roles := func(rs []MemberRole) string {
		var out []string
		for _, r := range rs {
			x := string(r.Role)
			if r.ExpiresAt != nil {
				x += " until " + r.ExpiresAt.UTC().Format(time.DateOnly)
			}
			out = append(out, x)
		}
		return strings.Join(out, "; ")
	}
	for _, m := range r.Members {
		last := ""
		if m.LastSignIn != nil {
			last = m.LastSignIn.Format(time.RFC3339)
		}
		var cs []string
		for _, c := range m.Conflicts {
			cs = append(cs, c.SoD+" ("+c.Covered+")")
		}
		s.Rows = append(s.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Text("member"), sheet.Text(m.DisplayName), sheet.Text(roles(m.Roles)),
			sheet.Text(roles(m.ExpiredRoles)), sheet.Text(yes(m.External)), sheet.Text(m.JoinedAt.Format(time.RFC3339)), sheet.Text(last),
			sheet.Text(m.SecondFactor), sheet.Text(yes(m.Dormant)), sheet.Text(strings.Join(cs, "; ")), sheet.Text("")}})
	}
	for _, i := range r.Invitations {
		var rs []MemberRole
		for _, x := range i.Roles {
			rs = append(rs, MemberRole{Role: x, ExpiresAt: i.RolesExpireAt})
		}
		s.Rows = append(s.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Text("invitation"), sheet.Text(i.DisplayName), sheet.Text(roles(rs)), sheet.Text(""),
			sheet.Text(yes(i.RolesExpireAt != nil)), sheet.Text(i.CreatedAt.Format(time.RFC3339)), sheet.Text(""), sheet.Text(""), sheet.Text(""),
			sheet.Text(""), sheet.Text("")}})
	}
	for _, c := range r.Connectors {
		s.Rows = append(s.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Text("connector"), sheet.Text(c.Name), sheet.Text(joinScopes(c.Scopes)),
			sheet.Text(""), sheet.Text(""), sheet.Text(c.InstalledAt.Format(time.RFC3339)), sheet.Text(""), sheet.Text(""), sheet.Text(""),
			sheet.Text(""), sheet.Text(c.Health)}})
	}
	return s
}

func registerAccessReview(a *API) {
	register(a, authz.GetAccessReview, huma.Operation{
		OperationID: "getAccessReview", Method: http.MethodGet, Path: "/v1/access-review", Tags: []string{"Members"},
		Summary: "Read the user access review",
		Description: "Who can do what in the organisation, live (FR-CTL-06, SEC-06; rev 0.162): each member's roles, an external user's " +
			"end dates and roles that have ended, when they joined and last signed in and how that session passed its second factor, " +
			"whether they are dormant (no sign-in for 30 days), and the segregation-of-duties conflicts their roles hold with what " +
			"covers each under the team mode; the pending invitations; the organisation's connectors with their scopes and health. Also " +
			"the latest calendar quarter to attest, a quarter overdue (C.2 #7), and every attestation. The ETag covers who holds what, " +
			"not sign-ins, so an Owner's step-up before attesting keeps it. Read-only; no side effects.",
	}, []string{}, func(ctx context.Context, _ *struct{}) (*accessReviewOutput, error) {
		p, _ := authn.From(ctx)
		var out accessReviewOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out.Body, out.ETag, err = a.accessReviewOf(ctx, u)
			return err
		})
		return &out, err
	})

	register(a, authz.AttestAccessReview, huma.Operation{
		OperationID: "attestAccessReview", Method: http.MethodPost, Path: "/v1/access-review/attestations", Tags: []string{"Members"},
		Summary: "Attest the user access review for a quarter",
		Description: "An Owner attests that they reviewed who can do what (FR-CTL-06, SEC-06; rev 0.162), for the latest calendar " +
			"quarter that has ended, once; an attestation answers for the quarters before it too, so C.2 #7 stops warning. Needs a person " +
			"who passed step-up in the last 5 minutes and If-Match with the review's ETag, so what is attested is what they saw. Side " +
			"effects: the attestation with the report as canonical JSON, whose SHA-256 the database computes and keeps, never changed " +
			"(FY247); its audit event. CHECK-03 when no quarter has ended since the organisation began, the quarter named is not the " +
			"one to attest, or it is attested already. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves " +
			"nothing.",
	}, []string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, accessreview.CodeAttest, storage.CodeDuplicate, storage.CodeAccessReview},
		func(ctx context.Context, in *attestAccessReviewInput) (*accessReviewWriteOutput, error) {
			quarter, err := time.Parse(time.DateOnly, in.Body.Quarter)
			if err != nil || quarter != accessreview.QuarterOf(quarter) {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.quarter", Message: "is not the first day of a calendar quarter",
					Fix: "Send to_attest.quarter from getAccessReview, such as 2026-07-01"}})
			}
			note := strings.TrimSpace(in.Body.Note)
			if len([]rune(note)) > 1000 {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.note", Message: "is longer than 1,000 characters", Fix: "Shorten it"}})
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			if _, err := a.steppedUpPerson(ctx, "Attesting the access review"); err != nil {
				return nil, err
			}
			var tag string
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AccessReview, error) {
				r, current, err := a.accessReviewOf(ctx, u)
				if err != nil {
					return AccessReview{}, err
				}
				if err := checkIfMatch(in.IfMatch, current); err != nil {
					return AccessReview{}, err
				}
				switch {
				case r.ToAttest == nil:
					return AccessReview{}, problem(accessreview.CodeAttest, "There is no quarter to attest: the latest one that ended is attested "+
						"already, or none has ended since the organisation began.", "Attest again once the current quarter ends")
				case r.ToAttest.Quarter != in.Body.Quarter:
					return AccessReview{}, problem(accessreview.CodeAttest, "The quarter to attest is "+r.ToAttest.Label+", not the one sent.",
						"Send to_attest.quarter from getAccessReview")
				}
				report, err := json.Marshal(r)
				if err != nil {
					return AccessReview{}, err
				}
				if _, err := u.AccessReviews().Attest(ctx, storage.NewAccessReviewAttestation{Quarter: quarter, AttestedBy: uploader(ctx),
					AttestedAt: a.deps.Clock.Now(), Note: note, Report: string(report)}); err != nil {
					return AccessReview{}, err
				}
				r, tag, err = a.accessReviewOf(ctx, u)
				return r, err
			})
			if err != nil {
				return nil, err
			}
			out := &accessReviewWriteOutput{Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				if tag == "" { // a replay: the saved answer's version
					if tag, err = accessReviewETag(w.Body); err != nil {
						return nil, err
					}
				}
				out.ETag, out.Replayed = tag, replayedHeader(w.Replayed)
			}
			return out, nil
		})

	register(a, authz.ExportAccessReview, huma.Operation{
		OperationID: "exportAccessReview", Method: http.MethodGet, Path: "/v1/access-review/export", Tags: []string{"Members"},
		Summary: "Export the user access review",
		Description: "The review getAccessReview gives, as CSV (FR-CTL-06): a row per member, pending invitation and connector; text " +
			"cells quoted so none becomes a formula. Returned in base64 with its SHA-256. Side effects: the export's audit event " +
			"(access_review.exported), as every export of personal data has.",
	}, []string{}, func(ctx context.Context, _ *struct{}) (*accessReviewFileOutput, error) {
		p, _ := authn.From(ctx)
		var r AccessReview
		var data []byte
		id := uuid.Must(uuid.NewV7())
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			if r, _, err = a.accessReviewOf(ctx, u); err != nil {
				return err
			}
			data = sheet.CSV(accessReviewSheet(r))
			sum := sha256.Sum256(data)
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "access_review.exported", ObjectType: "report", ObjectID: id,
				Changes: map[string][2]any{"export": {nil, map[string]any{"report": "access_review", "format": "csv", "members": len(r.Members),
					"sha256": hex.EncodeToString(sum[:])}}}})
		})
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		rows := len(r.Members) + len(r.Invitations) + len(r.Connectors)
		return &accessReviewFileOutput{Body: ReportFile{ExportID: id.String(), FileName: "access-review-" + r.AsAt.Format(time.DateOnly) + ".csv",
			MediaType: mediaCSV, SHA256: hex.EncodeToString(sum[:]), Size: len(data), Rows: rows, Content: base64.StdEncoding.EncodeToString(data)}}, nil
	})
}

// joinScopes are a connector's scopes as one cell.
func joinScopes(xs []ScopeName) string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = string(x)
	}
	return strings.Join(out, "; ")
}
