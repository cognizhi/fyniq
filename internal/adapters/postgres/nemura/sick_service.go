package nemura

import (
	"bytes"
	"context"
	"crypto/sha256"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/ports"
)

// SickService is sick leave behind the API (NM-LV-06, NM-LV-12): each call is one transaction for the caller's organisation, with the audit events of its changes.
// The certificate is a restricted document of the person, written to the blob store in the same unit of work; without a blob store the certificate answers
// api.ErrUnavailable and the notification still works.
type SickService struct {
	Store *Store
	Clock ports.Clock
	Packs *PacksService
	Blobs ports.BlobStore
}

var _ api.Sick = SickService{}

func (l SickService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return l.Store.InOrg(ctx, s.Organisation, fn)
}

func (l SickService) Notify(ctx context.Context, s api.Session, a api.SickAsk) (api.SickNotified, error) {
	var out api.SickNotified
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		from, err := time.Parse(time.DateOnly, a.From)
		if err != nil {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "from is not a date: write it as 2026-05-04."}
		}
		to, err := time.Parse(time.DateOnly, a.To)
		if err != nil {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "to is not a date: write it as 2026-05-06."}
		}
		var person uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT id FROM nemura.person WHERE member_id = $1", s.Member).Scan(&person); err != nil {
			return &people.Refusal{Code: people.CodeNotFound, Message: "Your login is linked to no person record: ask the people team to link it."}
		}
		v, ok := l.Packs.inForce("sg-people", from)
		if !ok {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "No country pack covers those dates: choose days from 2026 on."}
		}
		r, err := NotifySick(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, v, SickAsk{Person: person, Kind: a.Kind, From: from, To: to, Note: a.Note})
		out = api.SickNotified{ID: r.ID.String(), Days: r.Days.String(), Paid: r.Paid.String(), Unpaid: r.Unpaid.String(), Late: r.Late}
		return err
	})
	return out, err
}

func (l SickService) List(ctx context.Context, s api.Session, all, unverified bool) ([]api.SickNotification, error) {
	out := []api.SickNotification{}
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := Sick(ctx, tx, SickScope{Member: s.Member, All: all, Unverified: unverified}, nil)
		for _, r := range rows {
			out = append(out, api.SickNotification{SickNotified: api.SickNotified{ID: r.ID.String(), Days: r.Days.String(), Paid: r.Paid.String(), Unpaid: r.Unpaid.String(), Late: r.Late},
				PersonID: r.Person.String(), Person: r.PersonName, Kind: r.Kind, From: day(r.From), To: day(r.To), Note: r.Note, Certificate: r.Certificate != nil, Issuer: r.Issuer, Verified: r.Verified})
		}
		return err
	})
	return out, err
}

// Certificate stores the certificate as a restricted document the HR officer and the HR administrator see, and points the notification at it, in one transaction.
func (l SickService) Certificate(ctx context.Context, s api.Session, id uuid.UUID, team bool, issuer, contentType string, content []byte) error {
	if l.Blobs == nil {
		return api.ErrUnavailable
	}
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var person uuid.UUID
		var own bool
		err := tx.QueryRow(ctx, `SELECT n.person_id, coalesce(p.member_id = $2, false) FROM nemura.sick_notification n
JOIN nemura.person p ON p.organisation_id = n.organisation_id AND p.id = n.person_id WHERE n.id = $1`, id, s.Member).Scan(&person, &own)
		if err != nil || (!own && !team) {
			return &leave.Refusal{Code: leave.CodeNotFound, Message: "There is no such sick leave notification in your view."}
		}
		doc := uuid.Must(uuid.NewV7())
		key := "people/" + s.Organisation.String() + "/" + person.String() + "/" + doc.String()
		if err := l.Blobs.Create(ctx, key, bytes.NewReader(content)); err != nil {
			return err
		}
		if err := AddDocument(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, person, doc, DocumentInput{Type: "certificate", Title: "Medical certificate", RetentionClass: "employment-records",
			BlobKey: key, ContentType: contentType, Size: int64(len(content)), SHA256: sha256.Sum256(content), Viewers: []string{string("hr_officer"), string("hr_admin")}}); err != nil {
			return err
		}
		return AttachSickCertificate(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id, doc, issuer)
	})
}

func (l SickService) Verify(ctx context.Context, s api.Session, id uuid.UUID) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return VerifySick(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id)
	})
}
