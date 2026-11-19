package nemura

import (
	"bytes"
	"context"
	"crypto/sha256"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/selfservice"
	"github.com/cognizhi/fyniq/internal/ports"
)

// SelfService is a person's own details and requests behind the API (NM-ESS-03, NM-ESS-04). Without a blob store a change of details, which carries evidence, answers
// api.ErrUnavailable.
type SelfService struct {
	Store *Store
	Clock ports.Clock
	Blobs ports.BlobStore
}

var _ api.SelfService = SelfService{}

func (l SelfService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return l.Store.InOrg(ctx, s.Organisation, fn)
}

func (l SelfService) Details(ctx context.Context, s api.Session) (api.MyDetails, error) {
	var out api.MyDetails
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		p, err := OwnPerson(ctx, tx, s.Member)
		if err != nil {
			return err
		}
		d, err := DetailsOf(ctx, tx, p)
		if err != nil {
			return err
		}
		out = api.MyDetails{Phone: d.Contact.Phone, PersonalEmail: d.Contact.PersonalEmail, Address: d.Contact.Address, PostalCode: d.Contact.PostalCode, Emergency: []api.EmergencyContact{}}
		for _, e := range d.Emergency {
			out.Emergency = append(out.Emergency, api.EmergencyContact{Name: e.Name, Relationship: e.Relationship, Phone: e.Phone})
		}
		return nil
	})
	return out, err
}

func (l SelfService) SetDetails(ctx context.Context, s api.Session, d api.MyDetails) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		p, err := OwnPerson(ctx, tx, s.Member)
		if err != nil {
			return err
		}
		return SetDetails(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, p, selfservice.Contact{Phone: d.Phone, PersonalEmail: d.PersonalEmail, Address: d.Address, PostalCode: d.PostalCode})
	})
}

func (l SelfService) SetEmergency(ctx context.Context, s api.Session, list []api.EmergencyContact) error {
	in := make([]selfservice.EmergencyContact, len(list))
	for i, e := range list {
		in[i] = selfservice.EmergencyContact{Name: e.Name, Relationship: e.Relationship, Phone: e.Phone}
	}
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		p, err := OwnPerson(ctx, tx, s.Member)
		if err != nil {
			return err
		}
		return SetEmergencyContacts(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, p, in)
	})
}

// RequestChange stores the evidence as a document of the person, which they and the people team may read, and records the request in the same transaction.
func (l SelfService) RequestChange(ctx context.Context, s api.Session, c api.DetailsChange, content []byte) (string, string, error) {
	if l.Blobs == nil {
		return "", "", api.ErrUnavailable
	}
	var pr *time.Time
	if c.PRStart != "" {
		d, err := time.Parse(time.DateOnly, c.PRStart)
		if err != nil {
			return "", "", &selfservice.Refusal{Code: selfservice.CodeInvalid, Message: "pr_start is not a date: write it as 2024-03-01."}
		}
		pr = &d
	}
	ch := selfservice.Change{Kind: c.Kind, LegalName: c.LegalName, Residency: c.Residency, Note: c.Note, PRStart: pr}
	if err := selfservice.CheckChange(ch); err != nil {
		return "", "", err
	}
	var id uuid.UUID
	var due time.Time
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		person, err := OwnPerson(ctx, tx, s.Member)
		if err != nil {
			return err
		}
		doc := uuid.Must(uuid.NewV7())
		key := "people/" + s.Organisation.String() + "/" + person.String() + "/" + doc.String()
		if err := l.Blobs.Create(ctx, key, bytes.NewReader(content)); err != nil {
			return err
		}
		ctype := c.Evidence.ContentType
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		if err := AddDocument(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, person, doc, DocumentInput{Type: "other", Title: c.Evidence.Title, RetentionClass: "employment_record", BlobKey: key,
			ContentType: ctype, Size: int64(len(content)), SHA256: sha256.Sum256(content), Viewers: []string{"employee", "hr_officer", "hr_admin", "owner"}}); err != nil {
			return err
		}
		id, due, err = RequestChange(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, ChangeAsk{Person: person, Change: ch, Evidence: doc})
		return err
	})
	return id.String(), day(due), err
}

func (l SelfService) Ask(ctx context.Context, s api.Session, question string) (string, string, error) {
	var id uuid.UUID
	var due time.Time
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		person, err := OwnPerson(ctx, tx, s.Member)
		if err != nil {
			return err
		}
		id, due, err = AskHR(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, person, question)
		return err
	})
	return id.String(), day(due), err
}

func rowOf(r RequestRow) api.RequestRow {
	out := api.RequestRow{ID: r.ID, Kind: r.Kind, Title: r.Title, State: r.State, Step: r.Step, Asked: r.Asked.UTC().Format(time.RFC3339), Reply: r.Reply, Detail: r.Detail}
	if r.Due != nil {
		out.Due = day(*r.Due)
	}
	return out
}

func (l SelfService) Requests(ctx context.Context, s api.Session) ([]api.RequestRow, error) {
	out := []api.RequestRow{}
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		person, err := OwnPerson(ctx, tx, s.Member)
		if err != nil {
			return err
		}
		rows, err := RequestsOf(ctx, tx, person)
		for _, r := range rows {
			out = append(out, rowOf(r))
		}
		return err
	})
	return out, err
}

func (l SelfService) Withdraw(ctx context.Context, s api.Session, id uuid.UUID) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		person, err := OwnPerson(ctx, tx, s.Member)
		if err != nil {
			return err
		}
		return WithdrawOwn(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, person, id)
	})
}

func (l SelfService) Queue(ctx context.Context, s api.Session) ([]api.QueueRow, error) {
	out := []api.QueueRow{}
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := Queue(ctx, tx)
		for _, r := range rows {
			q := api.QueueRow{RequestRow: rowOf(r), PersonID: r.Person.String(), Person: r.PersonName, ChangeKind: r.ChangeKind}
			if r.Evidence != nil {
				q.EvidenceID = r.Evidence.String()
			}
			out = append(out, q)
		}
		return err
	})
	return out, err
}

func (l SelfService) Decide(ctx context.Context, s api.Session, id uuid.UUID, approve bool, reason string) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		_, err := DecideChange(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id, approve, reason)
		return err
	})
}

func (l SelfService) Reply(ctx context.Context, s api.Session, id uuid.UUID, reply string) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return AnswerHR(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id, reply)
	})
}
