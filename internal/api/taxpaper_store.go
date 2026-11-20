package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/taxfilings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the stored tax working paper (FR-TAX-06; p2-tax-filings task 2.3, rev 0.608)

// TaxPaperCodes are the stored paper's own codes.
var TaxPaperCodes = []string{taxfilings.CodePaper, taxfilings.CodeNoEvidence, taxfilings.CodeRecorded}

// TaxAdjustmentEntry is an adjustment the tax agent keeps with a paper.
type TaxAdjustmentEntry struct {
	Kind   string `json:"kind" enum:"non_deductible,capital_allowance_difference,other" doc:"non_deductible: an expense the Act does not allow, added back; capital_allowance_difference: the allowances less the depreciation, negative to take them off." example:"non_deductible"`
	Amount string `json:"amount" pattern:"^-?[0-9]{1,13}(\\.[0-9]{1,2})?$" doc:"In Singapore dollars: positive adds to the profit, negative takes off." example:"15000.00"`
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why: the tax agent signs the working; attach what supports it as evidence." example:"Client entertainment"`
}

// TaxPaperDraft is a working paper as the tax agent keeps it.
type TaxPaperDraft struct {
	FinancialYearEnd  string               `json:"financial_year_end" format:"date" doc:"The last day of the financial year, which must be a closed month's end in the books." example:"2026-12-31"`
	Scheme            string               `json:"scheme" enum:"sute,pte" doc:"sute: the start-up exemption; pte: the partial exemption." example:"pte"`
	CashGrantEligible bool                 `json:"cash_grant_eligible,omitempty" doc:"The company met the cash grant's local employee condition." example:"false"`
	Adjustments       []TaxAdjustmentEntry `json:"adjustments" maxItems:"100" nullable:"false" doc:"What the tax agent adds to or takes off the profit."`
}

// TaxPaperView is a stored paper, drafted or recorded.
type TaxPaperView struct {
	ID         string           `json:"id" format:"uuid" doc:"The paper's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8c01"`
	Status     string           `json:"status" enum:"draft,recorded,superseded" doc:"draft: waiting for its evidence and to be recorded; recorded: the latest recorded of its year; superseded: a later one is recorded." example:"draft"`
	Version    int              `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Draft      TaxPaperDraft    `json:"paper" doc:"What the tax agent entered."`
	Evidence   int              `json:"evidence" doc:"How many files are attached and not removed: recording a paper with adjustments needs one." example:"1"`
	RecordedBy *Person          `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt *time.Time       `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-11-20T09:30:00Z"`
	Figures    *TaxWorkingPaper `json:"figures,omitempty" doc:"What the paper works out: from the ledger now while a draft, and as it was when recorded once recorded; absent when it cannot be worked (the checks say why)."`
	Checks     []BillCheck      `json:"checks" nullable:"false" doc:"A draft's status: ✖ while it has adjustments and no evidence."`
}

// TaxPaperPage is a page of papers, the latest recorded first, then the drafts.
type TaxPaperPage struct {
	Items      []TaxPaperView `json:"items" nullable:"false"`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

type (
	// TaxPaperPath names a stored paper in the path.
	TaxPaperPath struct {
		PaperID string `path:"paper_id" format:"uuid" doc:"The stored tax working paper." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8c01"`
	}
	// TaxPaperEvidencePath names one of its files.
	TaxPaperEvidencePath struct {
		TaxPaperPath
		EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	}
	taxPaperListInput struct {
		PageQuery
		FinancialYearEnd string `query:"financial_year_end" format:"date" doc:"Only the papers of this financial year." example:"2026-12-31"`
	}
	taxPaperListOutput struct{ Body TaxPaperPage }
	taxPaperDraftInput struct {
		KeyHeader
		PreferHeader
		Body TaxPaperDraft
	}
	taxPaperReviseInput struct {
		TaxPaperPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body TaxPaperDraft
	}
	taxPaperRecordInput struct {
		TaxPaperPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	taxPaperReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body TaxPaperView
	}
	taxPaperWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     TaxPaperView
	}
	addTaxPaperEvidenceInput struct {
		TaxPaperPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[EvidenceUpload]
	}
	listTaxPaperEvidenceInput struct {
		TaxPaperPath
		PageQuery
	}
	removeTaxPaperEvidenceInput struct {
		TaxPaperEvidencePath
		IfMatchHeader
		PreferHeader
	}
)

func taxStoredIn(d TaxPaperDraft) (taxfilings.Stored, error) {
	s := taxfilings.Stored{Scheme: d.Scheme, CashGrantEligible: d.CashGrantEligible}
	var fields []FieldProblem
	if day := dayIn("body.financial_year_end", d.FinancialYearEnd, &fields); day != nil {
		s.FinancialYearEnd = *day
	}
	if len(fields) > 0 {
		return s, invalid(fields)
	}
	for _, x := range d.Adjustments {
		s.Adjustments = append(s.Adjustments, taxfilings.Adjustment{Kind: x.Kind, Amount: x.Amount, Reason: x.Reason})
	}
	return s, nil
}

func taxPaperProblems(err error) error {
	var lp ledger.Problems
	if errors.As(err, &lp) && len(lp) > 0 {
		fields := make([]FieldProblem, 0, len(lp))
		for _, x := range lp {
			fields = append(fields, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
		}
		return problem(lp[0].Code, "The working paper does not fit: each problem is listed in errors.", "Correct the paper and ask again.", fields...)
	}
	return err
}

func taxPaperETag(v TaxPaperView) string {
	status := "draft"
	if v.Status != "draft" {
		status = "posted"
	}
	return etag(v.Version, status)
}

func (a *API) taxPaperOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s taxfilings.Stored, latest map[time.Time]uuid.UUID) (TaxPaperView, error) {
	out := TaxPaperView{ID: doc.ID.String(), Status: "draft", Version: doc.Version, Draft: TaxPaperDraft{FinancialYearEnd: dateOf(s.FinancialYearEnd), Scheme: s.Scheme,
		CashGrantEligible: s.CashGrantEligible, Adjustments: []TaxAdjustmentEntry{}}}
	for _, x := range s.Adjustments {
		out.Draft.Adjustments = append(out.Draft.Adjustments, TaxAdjustmentEntry{Kind: x.Kind, Amount: x.Amount, Reason: x.Reason})
	}
	switch {
	case doc.Status == storage.DocumentPosted && latest[s.FinancialYearEnd] == doc.ID:
		out.Status = "recorded"
	case doc.Status == storage.DocumentPosted:
		out.Status = "superseded"
	}
	var err error
	out.Evidence, _, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, doc, nil)
	if err != nil {
		return out, err
	}
	for k := range out.Checks {
		if out.Checks[k].Code != "" {
			out.Checks[k].Code = taxfilings.CodeNoEvidence
			out.Checks[k].Message = "it has adjustments and no evidence attached, so it cannot be recorded"
			out.Checks[k].Fix = "Attach what supports the adjustments, such as the invoices of the non-deductible expenses"
		}
	}
	if len(s.Adjustments) == 0 { // nothing to evidence
		out.Checks = []BillCheck{}
	}
	switch {
	case len(s.Figures) > 0:
		var f TaxWorkingPaper
		if json.Unmarshal(s.Figures, &f) == nil {
			out.Figures = &f
		}
	case doc.Status == storage.DocumentDraft:
		if f, err := a.taxPaperOfStored(ctx, u, s, out.Evidence > 0); err == nil {
			out.Figures = &f
		}
	}
	return out, nil
}

// taxPaperOfStored works a stored paper out from the ledger.
func (a *API) taxPaperOfStored(ctx context.Context, u storage.UnitOfWork, s taxfilings.Stored, evidence bool) (TaxWorkingPaper, error) {
	req := TaxWorkingPaperRequest{FinancialYearEnd: dateOf(s.FinancialYearEnd), Scheme: s.Scheme, CashGrantEligible: s.CashGrantEligible}
	for _, x := range s.Adjustments {
		req.Adjustments = append(req.Adjustments, TaxAdjustment{Kind: x.Kind, Amount: x.Amount, Reason: x.Reason, Evidence: evidence})
	}
	return a.taxPaper(ctx, u, s.FinancialYearEnd, req)
}

// latestPapers is the latest recorded paper of each financial year.
func latestPapers(rs []taxfilings.Recorded) map[time.Time]uuid.UUID {
	out := map[time.Time]uuid.UUID{}
	for _, r := range rs { // the latest recorded first
		if r.Doc.Status == storage.DocumentPosted {
			if _, ok := out[r.FinancialYearEnd]; !ok {
				out[r.FinancialYearEnd] = r.Doc.ID
			}
		}
	}
	return out
}

func (a *API) shownTaxPaper(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (TaxPaperView, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return TaxPaperView{}, err
	}
	if doc.Type != storage.DocumentTaxWorkingPaper {
		return TaxPaperView{}, ports.ErrNotFound
	}
	s, err := taxfilings.Read(doc)
	if err != nil {
		return TaxPaperView{}, err
	}
	rs, err := taxfilings.Papers(ctx, u, s.FinancialYearEnd)
	if err != nil {
		return TaxPaperView{}, err
	}
	return a.taxPaperOf(ctx, u, doc, s, latestPapers(rs))
}

func taxPaperAnswer(w written[TaxPaperView]) *taxPaperWriteOutput {
	if w.Previewed {
		return &taxPaperWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &taxPaperWriteOutput{Status: w.Status, ETag: taxPaperETag(w.Body), Location: "/v1/tax/working-papers/" + w.Body.ID, Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

func registerTaxPapers(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.ListTaxWorkingPapers, huma.Operation{
		OperationID: "listTaxWorkingPapers", Method: http.MethodGet, Path: "/v1/tax/working-papers", Tags: []string{"Compliance"},
		Summary:     "List the stored tax working papers",
		Description: "The papers the tax agent keeps, the latest recorded first, then the drafts, a page at a time, optionally of one financial year (FR-TAX-06). Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, locks...), func(ctx context.Context, in *taxPaperListInput) (*taxPaperListOutput, error) {
		var end time.Time
		if in.FinancialYearEnd != "" {
			var err error
			if end, err = time.Parse(time.DateOnly, in.FinancialYearEnd); err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "financial_year_end", Message: "is not a date", Fix: "Send the financial year's last day, YYYY-MM-DD"}})
			}
		}
		p, _ := authn.From(ctx)
		out := taxPaperListOutput{Body: TaxPaperPage{Items: []TaxPaperView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rs, err := taxfilings.Papers(ctx, u, end)
			if err != nil {
				return err
			}
			latest := latestPapers(rs)
			var all []TaxPaperView
			for _, r := range rs {
				v, err := a.taxPaperOf(ctx, u, r.Doc, r.Stored, latest)
				if err != nil {
					return err
				}
				all = append(all, v)
			}
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x TaxPaperView) string { return x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []TaxPaperView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateTaxWorkingPaper, huma.Operation{
		OperationID: "createTaxWorkingPaper", Method: http.MethodPost, Path: "/v1/tax/working-papers", Tags: []string{"Compliance"}, DefaultStatus: http.StatusCreated,
		Summary: "Draft a tax working paper",
		Description: "Keeps a financial year's adjustments with their reasons as a draft (FR-TAX-06), to attach their evidence to with addTaxWorkingPaperEvidence and then record with " +
			"recordTaxWorkingPaper, which keeps the figures worked out at that moment. The figures show on the draft, worked from the ledger now. Side effects: the draft, audit events. " +
			"Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), TaxPaperCodes...), func(ctx context.Context, in *taxPaperDraftInput) (*taxPaperWriteOutput, error) {
		s, err := taxStoredIn(in.Body)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (TaxPaperView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return TaxPaperView{}, err
			}
			doc, err := taxfilings.DraftIn(ctx, u, s, today)
			if err != nil {
				return TaxPaperView{}, taxPaperProblems(err)
			}
			return a.shownTaxPaper(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return taxPaperAnswer(w), nil
	})

	register(a, authz.GetTaxWorkingPaper, huma.Operation{
		OperationID: "getTaxWorkingPaper", Method: http.MethodGet, Path: "/v1/tax/working-papers/{paper_id}", Tags: []string{"Compliance"},
		Summary:     "Get a stored tax working paper",
		Description: "A paper, drafted or recorded, with its adjustments, its files, its figures and who recorded it (FR-TAX-06). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *TaxPaperPath) (*taxPaperReadOutput, error) {
		id, err := parseID("paper_id", in.PaperID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out taxPaperReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.shownTaxPaper(ctx, u, id)
			out.Body, out.ETag = v, taxPaperETag(v)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceTaxWorkingPaper, huma.Operation{
		OperationID: "replaceTaxWorkingPaper", Method: http.MethodPut, Path: "/v1/tax/working-papers/{paper_id}", Tags: []string{"Compliance"},
		Summary: "Change a drafted tax working paper",
		Description: "Saves a new version of a paper not recorded yet (FR-TAX-06); a recorded one changes no more (TAXPAPER-03): draft a new paper for the year instead. Side effects: the new " +
			"version, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), TaxPaperCodes...), func(ctx context.Context, in *taxPaperReviseInput) (*taxPaperWriteOutput, error) {
		id, err := parseID("paper_id", in.PaperID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		s, err := taxStoredIn(in.Body)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (TaxPaperView, error) {
			doc, err := taxfilings.Drafted(ctx, u, id)
			if err != nil {
				return TaxPaperView{}, taxPaperProblems(err)
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return TaxPaperView{}, err
			}
			if _, err := taxfilings.ReviseIn(ctx, u, id, s); err != nil {
				return TaxPaperView{}, taxPaperProblems(err)
			}
			return a.shownTaxPaper(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return taxPaperAnswer(w), nil
	})

	register(a, authz.RecordTaxWorkingPaper, huma.Operation{
		OperationID: "recordTaxWorkingPaper", Method: http.MethodPost, Path: "/v1/tax/working-papers/{paper_id}/record", Tags: []string{"Compliance"},
		Summary: "Record a tax working paper",
		Description: "Records a drafted paper (FR-TAX-06): the tax agent signs the working. A paper with adjustments needs evidence attached (TAXPAPER-02). The figures it works out now are kept " +
			"with it, and from then on it changes no more and its files are committed to the retention-locked store (FR-DOC-13); it supersedes an earlier recorded paper of the same " +
			"year. It posts nothing and files nothing. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), TaxPaperCodes...), func(ctx context.Context, in *taxPaperRecordInput) (*taxPaperWriteOutput, error) {
		id, err := parseID("paper_id", in.PaperID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (TaxPaperView, error) {
			doc, err := taxfilings.Drafted(ctx, u, id)
			if err != nil {
				return TaxPaperView{}, taxPaperProblems(err)
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return TaxPaperView{}, err
			}
			s, err := taxfilings.Read(doc)
			if err != nil {
				return TaxPaperView{}, err
			}
			files, _, _, _, _, err := a.recordCommon(ctx, u, doc, nil)
			if err != nil {
				return TaxPaperView{}, err
			}
			figures, err := a.taxPaperOfStored(ctx, u, s, files > 0)
			if err != nil {
				return TaxPaperView{}, err
			}
			raw, err := json.Marshal(figures)
			if err != nil {
				return TaxPaperView{}, err
			}
			if _, err := taxfilings.RecordIn(ctx, u, id, raw); err != nil {
				return TaxPaperView{}, taxPaperProblems(err)
			}
			if a.deps.Outbox != nil { // its evidence committed now, not at the next sweep (FR-DOC-13)
				p, _ := authn.From(ctx)
				if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
					return TaxPaperView{}, err
				}
			}
			return a.shownTaxPaper(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return taxPaperAnswer(w), nil
	})

	evidenceLocks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	addCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, equity.CodeRecorded}, append(evidenceLocks, EvidenceCodes...)...)
	removeCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, equity.CodeRecorded}, evidenceLocks...)
	register(a, authz.AddTaxWorkingPaperEvidence, huma.Operation{
		OperationID: "addTaxWorkingPaperEvidence", Method: http.MethodPost, Path: "/v1/tax/working-papers/{paper_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach what supports a tax working paper's adjustments",
		Description: "Attaches a file to a drafted paper (FR-TAX-06, FR-DOC-05), as multipart/form-data: the invoices of the non-deductible expenses, the capital allowance schedule or the " +
			"tax agent's note. Recording a paper with adjustments needs one. The file's type is decided from its content; it is scanned for malware and its image metadata is stripped " +
			"before it is stored (FR-DOC-08). Side effects: the file in the staging store, its evidence row and its audit event; once recorded its files are committed to the " +
			"retention-locked store (FR-DOC-13). Needs If-Match with the paper's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, addCodes, func(ctx context.Context, in *addTaxPaperEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.attachEquityEvidence(ctx, storage.DocumentTaxWorkingPaper, "/v1/tax/working-papers", in.PaperID, in.IdempotencyKey, in.IfMatch, in.Prefer, in.RawBody.Data())
	})
	register(a, authz.ListTaxWorkingPaperEvidence, huma.Operation{
		OperationID: "listTaxWorkingPaperEvidence", Method: http.MethodGet, Path: "/v1/tax/working-papers/{paper_id}/evidence", Tags: []string{"Evidence"},
		Summary:     "List a tax working paper's files",
		Description: "The files attached to a paper, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *listTaxPaperEvidenceInput) (*equityEvidenceListOutput, error) {
		return a.listEquityEvidence(ctx, storage.DocumentTaxWorkingPaper, "paper_id", in.PaperID, in.PageQuery)
	})
	register(a, authz.GetTaxWorkingPaperEvidence, huma.Operation{
		OperationID: "getTaxWorkingPaperEvidence", Method: http.MethodGet, Path: "/v1/tax/working-papers/{paper_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get a tax working paper's file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *TaxPaperEvidencePath) (*equityEvidenceReadOutput, error) {
		return a.getEquityEvidence(ctx, storage.DocumentTaxWorkingPaper, "paper_id", in.PaperID, in.EvidenceID)
	})
	register(a, authz.RemoveTaxWorkingPaperEvidence, huma.Operation{
		OperationID: "removeTaxWorkingPaperEvidence", Method: http.MethodDelete, Path: "/v1/tax/working-papers/{paper_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary: "Remove a file from a drafted tax working paper",
		Description: "Takes a file off a paper not recorded yet (FR-DOC-07). Once recorded its evidence is append-only, and the database refuses. The row stays, marked removed, for the " +
			"audit trail. Side effects: the removal and its audit event. Needs If-Match with the file's ETag; Prefer: return=preview saves nothing.",
	}, removeCodes, func(ctx context.Context, in *removeTaxPaperEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.removeEquityEvidence(ctx, storage.DocumentTaxWorkingPaper, "paper_id", in.PaperID, in.EvidenceID, in.IfMatch, in.Prefer, in)
	})
	if op := a.OpenAPI().Paths["/v1/tax/working-papers/{paper_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}
}
