package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ directors' pay choices (FR-EQ-04; p1-founders-money)
//
// A director's or shareholder's pay choice from a month on (no salary, salary paid, salary deferred, director's fee, or
// shares instead of pay) is drafted by an Owner or the Controller (A on "Director accounts"), takes the signed decision
// as its evidence, and is recorded: append-only from then on, posting no entry. A later choice supersedes it from its
// month; with none, the choice is "No salary" (recommended default 16, FR-ORG-08). The pay run's part comes with payroll
// (p1-payroll-link).

// PayChoiceCodes are the codes a pay choice's own checks answer.
var PayChoiceCodes = []string{director.CodePayChoice, director.CodeNotPerson, director.CodeNotPayChoice}

// DirectorPayChoiceDraft is a pay choice as drafted.
type DirectorPayChoiceDraft struct {
	ContactID string `json:"contact_id" format:"uuid" doc:"The director or shareholder: a contact marked so." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	From      string `json:"from" format:"date" doc:"The month it applies from: any day of it; it is kept as the month's first day." example:"2026-01-01"`
	Choice    string `json:"choice" enum:"no_salary,salary_paid,salary_deferred,director_fee,shares_instead_of_pay" doc:"no_salary; salary_paid through payroll; salary_deferred: pay owed on 2120 instead of paid, with CPF and tax still due; director_fee voted by the shareholders; shares_instead_of_pay, whose equity transaction comes with FR-EQ-17." example:"no_salary"`
	Note      string `json:"note,omitempty" maxLength:"1000" doc:"Why, or what the decision says." example:"Board minute of 2 Jan 2026: no salary until the seed round."`
}

// DirectorPayChoice is a pay choice, drafted or recorded.
type DirectorPayChoice struct {
	ID         string      `json:"id" format:"uuid" doc:"Its ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fc1"`
	Status     string      `json:"status" enum:"draft,recorded" doc:"draft: waiting for its signed decision to be attached and recorded; recorded: in force from its month until a later one." example:"recorded"`
	Version    int         `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	ContactID  string      `json:"contact_id" format:"uuid" doc:"The director or shareholder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	LegalName  string      `json:"legal_name" doc:"Their name." example:"Mei Lin Tan"`
	From       string      `json:"from" format:"date" doc:"The first day of the month it applies from." example:"2026-01-01"`
	Choice     string      `json:"choice" enum:"no_salary,salary_paid,salary_deferred,director_fee,shares_instead_of_pay" doc:"The choice." example:"no_salary"`
	Note       string      `json:"note,omitempty" doc:"Its note." example:"Board minute of 2 Jan 2026: no salary until the seed round."`
	Decisions  int         `json:"decisions" doc:"How many files are attached and not removed: recording needs one, the signed decision." example:"1"`
	RecordedBy *Person     `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt *time.Time  `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-01-02T09:30:00Z"`
	Checks     []BillCheck `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no signed decision is attached."`
}

// PayChoiceInForce is the pay choice in force for a person in a month.
type PayChoiceInForce struct {
	Choice     string     `json:"choice" enum:"no_salary,salary_paid,salary_deferred,director_fee,shares_instead_of_pay" doc:"The choice in force." example:"no_salary"`
	Default    bool       `json:"default" doc:"No choice is recorded for the month, so it is the default, no salary (FR-ORG-08)." example:"false"`
	From       string     `json:"from,omitempty" format:"date" doc:"The month the recorded choice applies from." example:"2026-01-01"`
	ChoiceID   string     `json:"choice_id,omitempty" format:"uuid" doc:"The recorded choice." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fc1"`
	RecordedAt *time.Time `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-01-02T09:30:00Z"`
}

// DirectorPayChoicePage is a page of pay choices, the latest month first.
type DirectorPayChoicePage struct {
	Items      []DirectorPayChoice `json:"items" nullable:"false"`
	NextCursor string              `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMDEtMDF8MDE5MiJ9"`
}

// DirectorPayChoicePath names a pay choice in the path.
type DirectorPayChoicePath struct {
	ChoiceID string `path:"choice_id" format:"uuid" doc:"The pay choice's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fc1"`
}

type payChoiceListInput struct {
	PageQuery
	ContactID string `query:"contact_id" format:"uuid" doc:"Only this person's choices." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	Status    string `query:"status" enum:"draft,recorded" doc:"Only the choices with this status." example:"recorded"`
}

type payChoiceListOutput struct{ Body DirectorPayChoicePage }

type createPayChoiceInput struct {
	KeyHeader
	PreferHeader
	Body DirectorPayChoiceDraft
}

type replacePayChoiceInput struct {
	DirectorPayChoicePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body DirectorPayChoiceDraft
}

type recordPayChoiceInput struct {
	DirectorPayChoicePath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type payChoiceOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
	Location string `header:"Location" doc:"Its URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     DirectorPayChoice
}

type payChoiceReadOutput struct {
	ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
	Body DirectorPayChoice
}

func payChoiceETag(doc storage.Document) string { return etag(doc.Version, string(doc.Status)) }

// payChoiceIn reads a pay choice as sent, with every invalid field at once.
func payChoiceIn(d DirectorPayChoiceDraft) (director.PayChoice, error) {
	c := director.PayChoice{Choice: d.Choice, Note: strings.TrimSpace(d.Note)}
	var fields []FieldProblem
	var err error
	if c.ContactID, err = uuid.Parse(d.ContactID); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.contact_id", Message: "is not an ID", Fix: "Send the contact's ID"})
	}
	if c.From, err = time.Parse(time.DateOnly, d.From); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.from", Message: "is not a date", Fix: "Send a day of the month it applies from, as YYYY-MM-DD"})
	}
	if len(fields) > 0 {
		return c, invalid(fields)
	}
	return c, nil
}

// payChoiceOf is a pay choice as the API shows it; names are the directors' and shareholders' names by contact.
func (a *API) payChoiceOf(ctx context.Context, u storage.UnitOfWork, names map[uuid.UUID]string, doc storage.Document) (DirectorPayChoice, error) {
	c, err := director.ReadPayChoice(doc)
	if err != nil {
		return DirectorPayChoice{}, err
	}
	out := DirectorPayChoice{ID: doc.ID.String(), Status: "draft", Version: doc.Version, ContactID: c.ContactID.String(), LegalName: names[c.ContactID],
		From: dateOf(c.From), Choice: c.Choice, Note: c.Note, Checks: []BillCheck{}}
	if out.Decisions, err = director.Attached(ctx, u, doc.ID); err != nil {
		return DirectorPayChoice{}, err
	}
	if doc.Status == storage.DocumentPosted {
		out.Status, out.RecordedBy, out.RecordedAt = "recorded", personOf(doc.PostedBy), doc.PostedAt
		return out, a.name(ctx, u, out.RecordedBy)
	}
	if out.Decisions == 0 {
		out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: director.CodeNoDecision, Field: "decisions",
			Message: "it has no signed decision attached, so it cannot be recorded", Fix: "Attach the signed board decision or minute"})
	}
	return out, nil
}

// directorNames are the directors' and shareholders' names, by contact.
func directorNames(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID]string, error) {
	names := map[uuid.UUID]string{}
	for after := uuid.Nil; ; {
		page, err := u.Contacts().Page(ctx, after, "director", 500)
		if err != nil {
			return nil, err
		}
		for _, c := range page {
			names[c.ID] = c.LegalName
		}
		if len(page) < 500 {
			return names, nil
		}
		after = page[len(page)-1].ID
	}
}

// payChoiceInForce is a person's pay choice in force in the month of asAt, from every recorded choice.
func payChoiceInForce(choices []director.RecordedChoice, asAt time.Time) PayChoiceInForce {
	c := director.InForce(choices, asAt)
	if c == nil {
		return PayChoiceInForce{Choice: director.ChoiceNoSalary, Default: true}
	}
	at := c.RecordedAt
	return PayChoiceInForce{Choice: c.Choice.Choice, From: dateOf(c.Choice.From), ChoiceID: c.ID.String(), RecordedAt: &at}
}

// readablePayChoice is a pay choice the caller may read: not found for one whose right on "Director accounts" is
// qualified (an Employee's own).
func readablePayChoice(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Document, error) {
	if !authz.Decide(authz.GetDirectorPayChoice, p.Roles, p.AI()).Allowed {
		return storage.Document{}, ports.ErrNotFound
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return doc, err
	}
	if !director.IsPayChoice(doc) {
		return doc, ports.ErrNotFound
	}
	return doc, nil
}

func registerPayChoices(a *API) {
	readCodes := []string{CodeNotFound, director.CodeNotPayChoice, director.CodePayChoice}

	register(a, authz.ListDirectorPayChoices, huma.Operation{
		OperationID: "listDirectorPayChoices", Method: http.MethodGet, Path: "/v1/director-pay-choices", Tags: []string{"Founders"},
		Summary: "List directors' pay choices",
		Description: "The pay choices of the directors and shareholders, drafted and recorded, the latest month first, a page at a time, " +
			"narrowed by person or status (FR-EQ-04). The choice in force for each person is on their director account " +
			"(listDirectorAccounts). Needs the right to read every director's account. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *payChoiceListInput) (*payChoiceListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := payChoiceListOutput{Body: DirectorPayChoicePage{Items: []DirectorPayChoice{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			names, err := directorNames(ctx, u)
			if err != nil {
				return err
			}
			status := storage.DocumentStatus("")
			switch in.Status {
			case "draft":
				status = storage.DocumentDraft
			case "recorded":
				status = storage.DocumentPosted
			}
			for {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentDirectorPayChoice, Status: status, AfterDate: afterDate,
					AfterID: afterID, Limit: 500})
				if err != nil {
					return err
				}
				for _, doc := range docs {
					afterDate, afterID = doc.Date, &doc.ID
					if len(out.Body.Items) == in.PageSize {
						last := out.Body.Items[len(out.Body.Items)-1]
						out.Body.NextCursor = encodeCursor(last.From + "|" + last.ID)
						return nil
					}
					x, err := a.payChoiceOf(ctx, u, names, doc)
					if err != nil {
						return err
					}
					if in.ContactID != "" && x.ContactID != in.ContactID {
						continue
					}
					out.Body.Items = append(out.Body.Items, x)
				}
				if len(docs) < 500 {
					return nil
				}
			}
		})
		return &out, err
	})

	register(a, authz.GetDirectorPayChoice, huma.Operation{
		OperationID: "getDirectorPayChoice", Method: http.MethodGet, Path: "/v1/director-pay-choices/{choice_id}", Tags: []string{"Founders"},
		Summary:     "Get a director's pay choice",
		Description: "Returns a pay choice with its ETag: its person, month, choice and note, how many files are attached, and who recorded it (FR-EQ-04). Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *DirectorPayChoicePath) (*payChoiceReadOutput, error) {
		id, err := parseID("choice_id", in.ChoiceID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out payChoiceReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := readablePayChoice(ctx, u, p, id)
			if err != nil {
				return err
			}
			names, err := directorNames(ctx, u)
			if err != nil {
				return err
			}
			out.ETag = payChoiceETag(doc)
			out.Body, err = a.payChoiceOf(ctx, u, names, doc)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &out, nil
	})

	writeCodes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeRetry, storage.CodeLockTimeout}, PayChoiceCodes...)
	shown := func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (DirectorPayChoice, error) {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return DirectorPayChoice{}, err
		}
		names, err := directorNames(ctx, u)
		if err != nil {
			return DirectorPayChoice{}, err
		}
		return a.payChoiceOf(ctx, u, names, doc)
	}
	answer := func(w written[DirectorPayChoice]) *payChoiceOutput {
		if w.Previewed {
			return &payChoiceOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		status := "draft"
		if w.Body.Status == "recorded" {
			status = "posted"
		}
		return &payChoiceOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/director-pay-choices/" + w.Body.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}

	register(a, authz.CreateDirectorPayChoice, huma.Operation{
		OperationID: "createDirectorPayChoice", Method: http.MethodPost, Path: "/v1/director-pay-choices", Tags: []string{"Founders"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a director's pay choice",
		Description: "Drafts a director's or shareholder's pay choice from a month on (FR-EQ-04): no salary, salary paid, salary deferred, " +
			"director's fee, or shares instead of pay. Attach the signed decision with addDirectorPayChoiceEvidence, then record it " +
			"with recordDirectorPayChoice. Side effects: the draft, audit events. Every problem comes back together. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writeCodes, func(ctx context.Context, in *createPayChoiceInput) (*payChoiceOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (DirectorPayChoice, error) {
			c, err := payChoiceIn(in.Body)
			if err != nil {
				return DirectorPayChoice{}, err
			}
			doc, err := director.DraftPayChoiceIn(ctx, u, c)
			if err != nil {
				return DirectorPayChoice{}, err
			}
			return shown(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.ReplaceDirectorPayChoice, huma.Operation{
		OperationID: "replaceDirectorPayChoice", Method: http.MethodPut, Path: "/v1/director-pay-choices/{choice_id}", Tags: []string{"Founders"},
		Summary: "Change a drafted pay choice",
		Description: "Saves a new version of a pay choice not recorded yet, checked as drafting it is (FR-EQ-04). A recorded choice " +
			"changes no more (FOUNDER-14): record a new one from the month it changes. Side effects: the new version, audit events. " +
			"Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, director.CodeRecorded}, writeCodes...), func(ctx context.Context, in *replacePayChoiceInput) (*payChoiceOutput, error) {
		id, err := parseID("choice_id", in.ChoiceID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DirectorPayChoice, error) {
			doc, _, err := director.DraftedPayChoice(ctx, u, id)
			if err != nil {
				return DirectorPayChoice{}, err
			}
			if err := checkIfMatch(in.IfMatch, payChoiceETag(doc)); err != nil {
				return DirectorPayChoice{}, err
			}
			c, err := payChoiceIn(in.Body)
			if err != nil {
				return DirectorPayChoice{}, err
			}
			if _, err := director.RevisePayChoiceIn(ctx, u, id, c); err != nil {
				return DirectorPayChoice{}, err
			}
			return shown(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.RecordDirectorPayChoice, huma.Operation{
		OperationID: "recordDirectorPayChoice", Method: http.MethodPost, Path: "/v1/director-pay-choices/{choice_id}/record", Tags: []string{"Founders"},
		Summary: "Record a director's pay choice",
		Description: "Records a drafted pay choice (FR-EQ-04): in force from its month until a later one, append-only, posting no entry. " +
			"It needs the signed decision attached (FOUNDER-13). Side effects: the choice recorded, audit events. Needs If-Match with " +
			"its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, director.CodeRecorded, director.CodeNoDecision, storage.CodeDocumentStatus}, writeCodes...),
		func(ctx context.Context, in *recordPayChoiceInput) (*payChoiceOutput, error) {
			id, err := parseID("choice_id", in.ChoiceID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DirectorPayChoice, error) {
				doc, _, err := director.DraftedPayChoice(ctx, u, id)
				if err != nil {
					return DirectorPayChoice{}, err
				}
				if err := checkIfMatch(in.IfMatch, payChoiceETag(doc)); err != nil {
					return DirectorPayChoice{}, err
				}
				if _, err := director.RecordPayChoiceIn(ctx, u, id); err != nil {
					return DirectorPayChoice{}, err
				}
				if a.deps.Outbox != nil { // its signed decision committed now, not at the next sweep (FR-DOC-13)
					p, _ := authn.From(ctx)
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return DirectorPayChoice{}, err
					}
				}
				return shown(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return answer(w), nil
		})

	registerPayChoiceEvidence(a)
}
