package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the ACRA reconciliation (FR-EQ-26, C.2 #26)
//
// ACRA's register of members, as a business profile gives it, is typed in holder by holder and class by class with the
// profile's PDF attached (the owner's decision of 7 Nov 2026), and recorded by whoever may post on the cap table. The
// register is compared with the extract in force at the extract's day; every difference shows on C.2 #26 and is fixed
// in the register by a reversal citing the extract, or in the typing by a newer extract (design D11).

// AcraCodes are the codes an ACRA extract answers besides the common ones.
var AcraCodes = []string{equity.CodeAcra, equity.CodeHolderRecord, equity.CodeRecorded, storage.CodeAcraExtract}

// AcraHoldingDraft is one holding as ACRA's register of members lists it.
type AcraHoldingDraft struct {
	HolderID       string `json:"holder_id" format:"uuid" doc:"The holder: a contact marked shareholder or investor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	ClassAccountID string `json:"class_account_id" format:"uuid" doc:"The class, by its share-capital account: 3010 ordinary, 3020 preference [accountant to confirm for several preference series]." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	Shares         string `json:"shares" pattern:"^[1-9][0-9]{0,13}$" doc:"The shares the business profile lists, a whole number." example:"700000"`
}

// AcraExtractDraft is an ACRA extract as drafted.
type AcraExtractDraft struct {
	ProfileOn string             `json:"profile_on" format:"date" doc:"The day the business profile was generated, as printed on it." example:"2026-10-01"`
	Holdings  []AcraHoldingDraft `json:"holdings" nullable:"false" maxItems:"500" doc:"Each holder's shares of each class, typed in from the business profile."`
	Note      string             `json:"note,omitempty" maxLength:"1000" doc:"A note." example:"Business profile bought on 1 Oct 2026"`
}

// AcraHoldingView is a holding as typed in, by name.
type AcraHoldingView struct {
	AcraHoldingDraft
	HolderName string `json:"holder_name" doc:"The holder's name." example:"Ah Kow"`
	ClassCode  string `json:"class_code" doc:"The class's account code." example:"3010"`
}

// AcraExtractView is an extract, drafted or recorded.
type AcraExtractView struct {
	ID         string            `json:"id" format:"uuid" doc:"The extract's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7d01"`
	Status     string            `json:"status" enum:"draft,recorded,superseded" doc:"draft: waiting for its PDF and to be recorded; recorded: in force; superseded: a later business profile is recorded." example:"recorded"`
	Version    int               `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	ProfileOn  string            `json:"profile_on" format:"date" doc:"The business profile's day." example:"2026-10-01"`
	Holdings   []AcraHoldingView `json:"holdings" nullable:"false" doc:"The holdings typed in, by name."`
	Note       string            `json:"note,omitempty" doc:"A note." example:"Business profile bought on 1 Oct 2026"`
	Evidence   int               `json:"evidence" doc:"How many files are attached and not removed: recording needs one, the business profile's PDF." example:"1"`
	RecordedBy *Person           `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt *time.Time        `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-10-02T09:30:00Z"`
	Checks     []BillCheck       `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no PDF is attached."`
}

// AcraExtractPage is a page of extracts, the latest business profile first.
type AcraExtractPage struct {
	Items      []AcraExtractView `json:"items" nullable:"false"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

// AcraDifferenceView is a holding where the register and ACRA differ on the extract's day (C.2 #26).
type AcraDifferenceView struct {
	HolderID       string `json:"holder_id" format:"uuid" doc:"The holder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName     string `json:"holder_name" doc:"Their name." example:"Lion Rock Ventures Pte. Ltd."`
	ClassAccountID string `json:"class_account_id" format:"uuid" doc:"The class's share-capital account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	ClassCode      string `json:"class_code" doc:"Its code." example:"3010"`
	Register       string `json:"register" doc:"The shares the register holds for them on the extract's day; 0 for none." example:"300000"`
	Acra           string `json:"acra" doc:"The shares ACRA's extract lists; 0 for none." example:"299000"`
}

// AcraReconciliation is the register against the extract in force (FR-EQ-26).
type AcraReconciliation struct {
	Extract     *AcraExtractView     `json:"extract,omitempty" doc:"The extract in force: the recorded one of the latest business profile; absent before one is recorded."`
	Differences []AcraDifferenceView `json:"differences" nullable:"false" doc:"Each holding where the register differs from it on its day, by holder then class: fix the register by a reversal citing the extract, or the typing by a newer extract."`
}

type (
	AcraExtractPath struct {
		ExtractID string `path:"extract_id" format:"uuid" doc:"The ACRA extract." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7d01"`
	}
	acraListInput  struct{ PageQuery }
	acraListOutput struct{ Body AcraExtractPage }
	acraDraftInput struct {
		KeyHeader
		PreferHeader
		Body AcraExtractDraft
	}
	acraReviseInput struct {
		AcraExtractPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body AcraExtractDraft
	}
	acraRecordInput struct {
		AcraExtractPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	acraReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body AcraExtractView
	}
	acraWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     AcraExtractView
	}
	acraReconcileOutput struct{ Body AcraReconciliation }
)

// acraIn reads an extract's fields, or what of them is wrong.
func acraIn(d AcraExtractDraft) (equity.AcraExtract, error) {
	x := equity.AcraExtract{Note: strings.TrimSpace(d.Note)}
	var fields []FieldProblem
	if day := dayIn("body.profile_on", d.ProfileOn, &fields); day != nil {
		x.ProfileOn = *day
	}
	for k, h := range d.Holdings {
		var y equity.AcraHolding
		y.Shares = strings.TrimSpace(h.Shares)
		var err error
		if y.HolderID, err = uuid.Parse(h.HolderID); err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("body.holdings[%d].holder_id", k), Message: "is not a contact's ID", Fix: "Send the holder's contact ID"})
		}
		if y.AccountID, err = uuid.Parse(h.ClassAccountID); err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("body.holdings[%d].class_account_id", k), Message: "is not an account's ID",
				Fix: "Send the share-capital account's ID"})
		}
		x.Holdings = append(x.Holdings, y)
	}
	if len(fields) > 0 {
		return x, invalid(fields)
	}
	return x, nil
}

// acraView is what extracts read: the holders' names and the accounts' codes.
type acraView struct {
	names map[uuid.UUID]string
	codes map[uuid.UUID]string
}

func acraViewOf(ctx context.Context, u storage.UnitOfWork) (acraView, error) {
	v := acraView{names: map[uuid.UUID]string{}, codes: map[uuid.UUID]string{}}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return v, err
	}
	for _, x := range accounts {
		v.codes[x.ID] = x.Code
	}
	return v, nil
}

func (v acraView) nameOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (string, error) {
	if n, ok := v.names[id]; ok {
		return n, nil
	}
	c, err := u.Contacts().Get(ctx, id)
	if err != nil {
		return "", err
	}
	v.names[id] = c.LegalName
	return c.LegalName, nil
}

func (a *API) acraExtractOf(ctx context.Context, u storage.UnitOfWork, v acraView, x equity.Recorded[equity.AcraExtract], latest *equity.Recorded[equity.AcraExtract]) (AcraExtractView, error) {
	out := AcraExtractView{ID: x.Doc.ID.String(), Status: "draft", Version: x.Doc.Version, ProfileOn: x.Value.ProfileOn.Format(time.DateOnly),
		Holdings: []AcraHoldingView{}, Note: x.Value.Note}
	switch {
	case x.Doc.Status == storage.DocumentPosted && latest != nil && latest.Doc.ID == x.Doc.ID:
		out.Status = "recorded"
	case x.Doc.Status == storage.DocumentPosted:
		out.Status = "superseded"
	}
	for _, h := range x.Value.Holdings {
		name, err := v.nameOf(ctx, u, h.HolderID)
		if err != nil {
			return out, err
		}
		out.Holdings = append(out.Holdings, AcraHoldingView{AcraHoldingDraft: AcraHoldingDraft{HolderID: h.HolderID.String(), ClassAccountID: h.AccountID.String(),
			Shares: h.Shares}, HolderName: name, ClassCode: v.codes[h.AccountID]})
	}
	var err error
	out.Evidence, _, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, x.Doc, nil)
	for k := range out.Checks {
		if out.Checks[k].Code == equity.CodeNoEvidence {
			out.Checks[k].Message = "it has no business profile attached, so it cannot be recorded"
			out.Checks[k].Fix = "Attach the business profile's PDF the holdings were typed from"
		}
	}
	return out, err
}

// shownAcra is an extract by document ID, as the caller reads it: not found for anything else.
func (a *API) shownAcra(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (AcraExtractView, error) {
	extracts, err := equity.AcraExtracts(ctx, u)
	if err != nil {
		return AcraExtractView{}, err
	}
	v, err := acraViewOf(ctx, u)
	if err != nil {
		return AcraExtractView{}, err
	}
	latest := equity.LatestAcra(extracts)
	for _, x := range extracts {
		if x.Doc.ID == id {
			return a.acraExtractOf(ctx, u, v, x, latest)
		}
	}
	return AcraExtractView{}, ports.ErrNotFound
}

func acraAnswer(w written[AcraExtractView]) *acraWriteOutput {
	if w.Previewed {
		return &acraWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	status := "draft"
	if w.Body.Status != "draft" {
		status = "posted"
	}
	return &acraWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/acra-extracts/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

func registerAcra(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.ListAcraExtracts, huma.Operation{
		OperationID: "listAcraExtracts", Method: http.MethodGet, Path: "/v1/acra-extracts", Tags: []string{"Equity"},
		Summary: "List the ACRA register extracts",
		Description: "Every ACRA register-of-members extract, drafts and recorded, the latest business profile first, a page at a time " +
			"(FR-EQ-26): its day, its holdings as typed in, its status and its files. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *acraListInput) (*acraListOutput, error) {
		p, _ := authn.From(ctx)
		out := acraListOutput{Body: AcraExtractPage{Items: []AcraExtractView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			extracts, err := equity.AcraExtracts(ctx, u)
			if err != nil {
				return err
			}
			v, err := acraViewOf(ctx, u)
			if err != nil {
				return err
			}
			latest := equity.LatestAcra(extracts)
			all := make([]AcraExtractView, 0, len(extracts))
			for _, x := range extracts {
				y, err := a.acraExtractOf(ctx, u, v, x, latest)
				if err != nil {
					return err
				}
				all = append(all, y)
			}
			slices.SortStableFunc(all, func(x, y AcraExtractView) int {
				if c := strings.Compare(y.ProfileOn, x.ProfileOn); c != 0 {
					return c
				}
				return strings.Compare(y.ID, x.ID)
			})
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x AcraExtractView) string { return x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []AcraExtractView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateAcraExtract, huma.Operation{
		OperationID: "createAcraExtract", Method: http.MethodPost, Path: "/v1/acra-extracts", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Type in an ACRA register extract",
		Description: "Drafts ACRA's register of members as a business profile gives it (FR-EQ-26, design D11; the owner's decision of " +
			"7 Nov 2026): the profile's day and each holder's shares of each class, typed in. Attach the business profile's PDF with " +
			"addAcraExtractEvidence, then record it with recordAcraExtract: the register is compared with it at its day, and each " +
			"difference shows on C.2 #26. Each holder is a contact marked shareholder or investor. Side effects: the draft, audit " +
			"events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), AcraCodes...), func(ctx context.Context, in *acraDraftInput) (*acraWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AcraExtractView, error) {
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return AcraExtractView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return AcraExtractView{}, err
			}
			x, err := acraIn(in.Body)
			if err != nil {
				return AcraExtractView{}, err
			}
			doc, err := equity.DraftAcraIn(ctx, u, x, s, today)
			if err != nil {
				return AcraExtractView{}, equityProblems(err)
			}
			return a.shownAcra(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return acraAnswer(w), nil
	})

	register(a, authz.GetAcraExtract, huma.Operation{
		OperationID: "getAcraExtract", Method: http.MethodGet, Path: "/v1/acra-extracts/{extract_id}", Tags: []string{"Equity"},
		Summary:     "Get an ACRA register extract",
		Description: "Returns an ACRA extract with its ETag: its day, its holdings by name, whether it is in force, and its files (FR-EQ-26). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *AcraExtractPath) (*acraReadOutput, error) {
		id, err := parseID("extract_id", in.ExtractID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out acraReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentAcraExtract, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownAcra(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceAcraExtract, huma.Operation{
		OperationID: "replaceAcraExtract", Method: http.MethodPut, Path: "/v1/acra-extracts/{extract_id}", Tags: []string{"Equity"},
		Summary: "Change a drafted ACRA register extract",
		Description: "Saves a new version of an extract not recorded yet (FR-EQ-26). A recorded one changes no more (EQUITY-05): type in a " +
			"newer business profile. Side effects: the new version, audit events. Needs If-Match with its ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), AcraCodes...), func(ctx context.Context, in *acraReviseInput) (*acraWriteOutput, error) {
		id, err := parseID("extract_id", in.ExtractID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AcraExtractView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentAcraExtract, id)
			if err != nil {
				return AcraExtractView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return AcraExtractView{}, err
			}
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return AcraExtractView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return AcraExtractView{}, err
			}
			x, err := acraIn(in.Body)
			if err != nil {
				return AcraExtractView{}, err
			}
			if _, err := equity.ReviseAcraIn(ctx, u, id, x, s, today); err != nil {
				return AcraExtractView{}, equityProblems(err)
			}
			return a.shownAcra(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return acraAnswer(w), nil
	})

	register(a, authz.RecordAcraExtract, huma.Operation{
		OperationID: "recordAcraExtract", Method: http.MethodPost, Path: "/v1/acra-extracts/{extract_id}/record", Tags: []string{"Equity"},
		Summary: "Record an ACRA register extract",
		Description: "Records a drafted extract (FR-EQ-26, design D11): in force from then on as the latest business profile, never " +
			"changed, until a newer one is recorded. It needs the business profile's PDF attached (EQUITY-06). It posts nothing: the " +
			"register is compared with it at its day, and C.2 #26 lists each difference. Side effects: the extract recorded, its files " +
			"committed, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, equity.CodeNoEvidence}, writes...), AcraCodes...),
		func(ctx context.Context, in *acraRecordInput) (*acraWriteOutput, error) {
			id, err := parseID("extract_id", in.ExtractID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AcraExtractView, error) {
				doc, err := equity.Drafted(ctx, u, storage.DocumentAcraExtract, id)
				if err != nil {
					return AcraExtractView{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return AcraExtractView{}, err
				}
				s, err := journal.LoadSettings(ctx, u)
				if err != nil {
					return AcraExtractView{}, err
				}
				if _, err := equity.RecordAcraIn(ctx, u, id, s); err != nil {
					return AcraExtractView{}, equityProblems(err)
				}
				if a.deps.Outbox != nil { // its evidence committed now, not at the next sweep (FR-DOC-13)
					p, _ := authn.From(ctx)
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return AcraExtractView{}, err
					}
				}
				return a.shownAcra(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return acraAnswer(w), nil
		})

	register(a, authz.GetAcraReconciliation, huma.Operation{
		OperationID: "getAcraReconciliation", Method: http.MethodGet, Path: "/v1/cap-table/acra-reconciliation", Tags: []string{"Equity"},
		Summary: "Get the register against ACRA's",
		Description: "The register of members against the ACRA extract in force (FR-EQ-26, design D11): the extract, and each holding where " +
			"the register's shares on the extract's day differ from those typed in, holder by holder and class by class, one listed by " +
			"only one side included. Fix the register by a reversal citing the extract, or the typing by a newer extract. With no extract " +
			"recorded, there is none and no difference. Read-only; no side effects.",
	}, locks, func(ctx context.Context, in *struct{}) (*acraReconcileOutput, error) {
		p, _ := authn.From(ctx)
		out := acraReconcileOutput{Body: AcraReconciliation{Differences: []AcraDifferenceView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			latest, diffs, err := equity.ReconcileIn(ctx, u)
			if err != nil || latest == nil {
				return err
			}
			v, err := acraViewOf(ctx, u)
			if err != nil {
				return err
			}
			x, err := a.acraExtractOf(ctx, u, v, *latest, latest)
			if err != nil {
				return err
			}
			out.Body.Extract = &x
			for _, d := range diffs {
				name, err := v.nameOf(ctx, u, d.HolderID)
				if err != nil {
					return err
				}
				out.Body.Differences = append(out.Body.Differences, AcraDifferenceView{HolderID: d.HolderID.String(), HolderName: name,
					ClassAccountID: d.AccountID.String(), ClassCode: v.codes[d.AccountID], Register: d.Register, Acra: d.Acra})
			}
			slices.SortStableFunc(out.Body.Differences, func(x, y AcraDifferenceView) int {
				if c := strings.Compare(strings.ToLower(x.HolderName), strings.ToLower(y.HolderName)); c != 0 {
					return c
				}
				return strings.Compare(x.ClassCode, y.ClassCode)
			})
			return nil
		})
		return &out, err
	})
}
