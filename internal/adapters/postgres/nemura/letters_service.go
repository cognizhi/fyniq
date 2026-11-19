package nemura

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/ports"
)

// LettersService is letters behind the API (NM-LTR-01 to NM-LTR-03, NM-LTR-07): each call is one transaction for the caller's organisation, with the audit events of its
// changes. Without a file store a letter cannot be checked, signed or read, and the call says so; the renderer is the local one or the platform's.
type LettersService struct {
	Packs    *PacksService // the country pack the key employment terms read; nil leaves the pack's facts out and names them when filling
	Store    *Store
	Clock    ports.Clock
	Blobs    ports.BlobStore
	Renderer letters.Renderer
}

var _ api.Letters = LettersService{}

func (l LettersService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return l.Store.InOrg(ctx, s.Organisation, fn)
}

func (l LettersService) Settings(ctx context.Context, s api.Session) (api.LetterSettings, bool, error) {
	var out api.LetterSettings
	var ok bool
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		v, set, err := LetterSettingsOf(ctx, tx)
		out, ok = api.LetterSettings{Name: v.Name, UEN: v.UEN, Address: v.Address}, set
		return err
	})
	return out, ok, err
}

func (l LettersService) SetSettings(ctx context.Context, s api.Session, v api.LetterSettings) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return SetLetterSettings(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, LetterSettings{Name: v.Name, UEN: v.UEN, Address: v.Address})
	})
}

func (l LettersService) Types(ctx context.Context, s api.Session) ([]api.LetterType, error) {
	out := []api.LetterType{}
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := LetterTypes(ctx, tx)
		for _, r := range rows {
			out = append(out, api.LetterType{Key: r.Type.Key, Title: r.Type.Title, Statement: r.Type.Statement, Enabled: r.Enabled, DueDays: r.DueDays, Version: r.Version, Needs: r.Type.Needs, Fields: r.Type.FieldKeys})
		}
		return err
	})
	return out, err
}

func (l LettersService) SetType(ctx context.Context, s api.Session, key string, enabled bool, dueDays int) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return SetLetterType(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, key, enabled, dueDays)
	})
}

func (l LettersService) Templates(ctx context.Context, s api.Session, key string) ([]api.LetterTemplate, error) {
	if _, ok := letters.TypeOf(key); !ok {
		return nil, &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter type in the catalogue."}
	}
	out := []api.LetterTemplate{}
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := TemplateVersions(ctx, tx, key)
		for _, r := range rows {
			t := api.LetterTemplate{ID: r.ID.String(), Type: r.Type, Version: r.Version, State: r.State, Note: r.Note, Blocks: []api.LetterBlock{}}
			for _, b := range r.Blocks {
				t.Blocks = append(t.Blocks, api.LetterBlock{Kind: b.Kind, Text: b.Text})
			}
			out = append(out, t)
		}
		return err
	})
	return out, err
}

func (l LettersService) SaveDraft(ctx context.Context, s api.Session, key string, blocks []api.LetterBlock, note string) (string, error) {
	var id uuid.UUID
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		bs := make([]letters.Block, len(blocks))
		for i, b := range blocks {
			bs[i] = letters.Block{Kind: b.Kind, Text: b.Text}
		}
		var err error
		id, err = SaveTemplateDraft(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, key, bs, note)
		return err
	})
	return id.String(), err
}

func (l LettersService) Publish(ctx context.Context, s api.Session, draft uuid.UUID) (int, error) {
	var v int
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		v, err = PublishTemplate(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, draft)
		return err
	})
	return v, err
}

func (l LettersService) pack(at time.Time) pack.Version {
	if l.Packs == nil {
		return pack.Version{}
	}
	v, _ := l.Packs.inForce("sg-people", at)
	return v
}

func (l LettersService) Request(ctx context.Context, s api.Session, typ, purpose string) (string, string, error) {
	var id uuid.UUID
	var due time.Time
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var person uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT id FROM nemura.person WHERE member_id = $1", s.Member).Scan(&person); err != nil {
			return &people.Refusal{Code: people.CodeNotFound, Message: "Your login is linked to no person record: ask the people team to link it."}
		}
		var err error
		id, due, err = RequestLetter(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, l.pack(l.Clock.Now()), LetterAsk{Person: person, Type: typ, Purpose: purpose})
		return err
	})
	return id.String(), day(due), err
}

// RequestFor is the people team asking for a letter about a person (NM-LTR-06 for the key employment terms).
func (l LettersService) RequestFor(ctx context.Context, s api.Session, person uuid.UUID, typ, purpose string) (string, string, error) {
	var id uuid.UUID
	var due time.Time
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := PersonOn(ctx, tx, person, l.Clock.Now()); err != nil {
			return err
		}
		var err error
		id, due, err = RequestLetter(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, l.pack(l.Clock.Now()), LetterAsk{Person: person, Type: typ, Purpose: purpose})
		return err
	})
	return id.String(), day(due), err
}

func dayStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return day(*t)
}

// Terms are a person's key employment terms on a day; the pay terms only when the caller may read them.
func (l LettersService) Terms(ctx context.Context, s api.Session, person uuid.UUID, on time.Time, pay bool) (api.Terms, bool, error) {
	var out api.Terms
	var ok bool
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := PersonOn(ctx, tx, person, on); err != nil {
			return err
		}
		t, found, err := TermsOn(ctx, tx, person, on)
		if err != nil || !found {
			return err
		}
		ok = true
		out = api.Terms{From: day(t.From), To: dayStr(t.To), Base: api.TermsBase{MainDuties: t.MainDuties, NoticePeriod: t.NoticePeriod, MedicalBenefits: t.MedicalBenefits, ContractEnd: dayStr(t.ContractEnd)}}
		if pay {
			out.Pay = &api.TermsPay{SalaryPeriod: t.SalaryPeriod, BasicSalary: t.BasicSalary, FixedAllowances: t.FixedAllowances, FixedDeductions: t.FixedDeductions, OvertimePeriod: t.OvertimePay, OtherPay: t.OtherPay}
		}
		return nil
	})
	return out, ok, err
}

// SetTerms starts a person's terms; a nil part carries forward.
func (l LettersService) SetTerms(ctx context.Context, s api.Session, person uuid.UUID, from time.Time, base *api.TermsBase, pay *api.TermsPay) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := PersonOn(ctx, tx, person, from); err != nil {
			return err
		}
		t := Terms{From: from}
		if base != nil {
			t.MainDuties, t.NoticePeriod, t.MedicalBenefits = base.MainDuties, base.NoticePeriod, base.MedicalBenefits
			if base.ContractEnd != "" {
				d, err := time.Parse(time.DateOnly, base.ContractEnd)
				if err != nil {
					return &letters.Refusal{Code: letters.CodeInvalid, Message: "contract_end is not a date: write it as 2027-08-31."}
				}
				t.ContractEnd = &d
			}
		}
		if pay != nil {
			t.SalaryPeriod, t.BasicSalary, t.FixedAllowances, t.FixedDeductions, t.OvertimePay, t.OtherPay = pay.SalaryPeriod, pay.BasicSalary, pay.FixedAllowances, pay.FixedDeductions, pay.OvertimePeriod, pay.OtherPay
		}
		return SetTerms(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, person, t, base != nil, pay != nil)
	})
}

func (l LettersService) List(ctx context.Context, s api.Session, all bool, state string) ([]api.LetterRequest, error) {
	out := []api.LetterRequest{}
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := LetterRequests(ctx, tx, LetterScope{Member: s.Member, All: all}, state)
		for _, r := range rows {
			v := api.LetterRequest{ID: r.ID.String(), PersonID: r.Person.String(), Person: r.PersonName, Type: r.Type, Purpose: r.Purpose, State: r.State, Due: day(r.Due),
				Requested: r.RequestedAt.UTC().Format(time.RFC3339), Reference: r.Reference, SHA256: r.SHA256, Reason: r.Reason}
			if r.Issued != nil {
				v.IssuedID = r.Issued.String()
			}
			if r.IssuedAt != nil {
				v.IssuedAt = r.IssuedAt.UTC().Format(time.RFC3339)
			}
			out = append(out, v)
		}
		return err
	})
	return out, err
}

func (l LettersService) Check(ctx context.Context, s api.Session, id uuid.UUID) (string, string, error) {
	if l.Renderer == nil {
		return "", "", api.ErrUnavailable
	}
	var ref string
	var sum [32]byte
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ref, sum, err = CheckLetter(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id, l.Renderer, l.Blobs)
		return err
	})
	return ref, hex.EncodeToString(sum[:]), err
}

func (l LettersService) Draft(ctx context.Context, s api.Session, id uuid.UUID) (string, []byte, error) {
	if l.Blobs == nil {
		return "", nil, api.ErrUnavailable
	}
	var ref string
	var pdf []byte
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var blob string
		var sum []byte
		var rp, bp *string
		if err := tx.QueryRow(ctx, "SELECT reference, render_blob, render_sha256 FROM nemura.letter_request WHERE id = $1", id).Scan(&rp, &bp, &sum); err != nil || rp == nil || bp == nil {
			return &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no checked letter for that request."}
		}
		ref, blob = *rp, *bp
		rc, err := l.Blobs.Get(ctx, blob)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		if pdf, err = io.ReadAll(rc); err != nil {
			return err
		}
		if got := sha256.Sum256(pdf); !bytes.Equal(got[:], sum) {
			pdf = nil
			return &letters.Refusal{Code: letters.CodeConflict, Message: "The stored letter no longer matches its hash: it is not shown, and the change is for the security team to look into."}
		}
		return nil
	})
	return ref, pdf, err
}

func (l LettersService) Sign(ctx context.Context, s api.Session, id uuid.UUID, sha string) (string, error) {
	raw, err := hex.DecodeString(sha)
	if err != nil || len(raw) != 32 {
		return "", &letters.Refusal{Code: letters.CodeInvalid, Message: "The hash is 64 hexadecimal characters: copy it from the letter as it was checked."}
	}
	var confirmed [32]byte
	copy(confirmed[:], raw)
	stepUp := l.Clock.Now()
	if s.StepUpAt != nil {
		stepUp = *s.StepUpAt
	}
	var issued uuid.UUID
	err = l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		issued, err = SignLetter(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id, confirmed, stepUp, "role:owner", l.Blobs)
		return err
	})
	return issued.String(), err
}

func (l LettersService) Refuse(ctx context.Context, s api.Session, id uuid.UUID, reason string, withdraw bool) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if withdraw { // only the person's own request
			var own bool
			if err := tx.QueryRow(ctx, "SELECT coalesce(p.member_id = $2, false) FROM nemura.letter_request q JOIN nemura.person p ON p.organisation_id = q.organisation_id AND p.id = q.person_id WHERE q.id = $1", id, s.Member).Scan(&own); err != nil || !own {
				return &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter request in your view."}
			}
		}
		return RefuseLetter(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id, reason, withdraw)
	})
}

// issuedOf is the issued letter of a request the caller may read: their own, or any for the people team.
func (l LettersService) issuedOf(ctx context.Context, tx pgx.Tx, s api.Session, id uuid.UUID, all bool) (uuid.UUID, error) {
	rows, err := LetterRequests(ctx, tx, LetterScope{Member: s.Member, All: all}, "issued")
	if err != nil {
		return uuid.Nil, err
	}
	for _, r := range rows {
		if r.ID == id && r.Issued != nil {
			return *r.Issued, nil
		}
	}
	return uuid.Nil, &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such issued letter in your view."}
}

func (l LettersService) File(ctx context.Context, s api.Session, id uuid.UUID, all bool) (string, []byte, error) {
	if l.Blobs == nil {
		return "", nil, api.ErrUnavailable
	}
	var ref string
	var pdf []byte
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		issued, err := l.issuedOf(ctx, tx, s, id, all)
		if err != nil {
			return err
		}
		ref, pdf, err = ReadLetter(ctx, tx, l.Blobs, issued)
		return err
	})
	return ref, pdf, err
}

func (l LettersService) Link(ctx context.Context, s api.Session, id uuid.UUID, all bool) (string, string, error) {
	var token string
	var exp time.Time
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		issued, err := l.issuedOf(ctx, tx, s, id, all)
		if err != nil {
			return err
		}
		token, exp, err = CreateLetterLink(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, issued)
		return err
	})
	return token, exp.UTC().Format(time.RFC3339), err
}

// ByLink delivers the letter a live token names: the token names the organisation, so no session is needed, and a token that is not valid is refused the same way for every reason.
func (l LettersService) ByLink(ctx context.Context, token string) (string, []byte, error) {
	org, ok := LinkOrganisation(token)
	if !ok || l.Blobs == nil {
		return "", nil, &letters.Refusal{Code: letters.CodeNotFound, Message: "This link is not valid: it may have expired. Ask for a new one in My requests."}
	}
	var ref string
	var pdf []byte
	err := l.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ref, pdf, err = LetterByLink(ctx, tx, l.Blobs, token, l.Clock.Now())
		return err
	})
	return ref, pdf, err
}
