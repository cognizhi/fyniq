package nemura

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/access"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/ports"
)

// RestrictedService is the restricted people data behind the API (NM-PPL-02 to NM-PPL-05, NM-PPL-07). Without a cipher the sealed fields answer
// api.ErrUnavailable; without a blob store documents do.
type RestrictedService struct {
	Store  *Store
	Cipher ports.Cipher
	Blobs  ports.BlobStore
	Clock  ports.Clock
}

var _ api.Restricted = RestrictedService{}

func (r RestrictedService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return r.Store.InOrg(ctx, s.Organisation, fn)
}

func (r RestrictedService) roles(ctx context.Context, tx pgx.Tx, s api.Session) ([]authz.Role, error) {
	return ActiveRoles(ctx, tx, s.Member, r.Clock.Now())
}

func (r RestrictedService) SetNRIC(ctx context.Context, s api.Session, id uuid.UUID, value string) error {
	if r.Cipher == nil {
		return api.ErrUnavailable
	}
	return r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return SetNRIC(ctx, tx, r.Cipher, s.Organisation, r.Clock.Now(), s.Member, id, value)
	})
}

func (r RestrictedService) Secret(ctx context.Context, s api.Session, id uuid.UUID, field string) (string, string, error) {
	if r.Cipher == nil {
		return "", "", api.ErrUnavailable
	}
	var v string
	var level access.Level
	var refusal error
	err := r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		held, err := r.roles(ctx, tx, s)
		if err != nil {
			return err
		}
		v, level, err = ReadSecret(ctx, tx, r.Cipher, s.Organisation, r.Clock.Now(), s.Member, held, id, field)
		if errors.Is(err, ErrHidden) {
			refusal = err
			return nil // the refusal's audit event is kept
		}
		return err
	})
	if err == nil {
		err = refusal
	}
	return v, level.String(), err
}

func (r RestrictedService) AddPass(ctx context.Context, s api.Session, id uuid.UUID, kind, number string, issued, expires time.Time) (uuid.UUID, error) {
	if r.Cipher == nil {
		return uuid.Nil, api.ErrUnavailable
	}
	var out uuid.UUID
	err := r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = AddWorkPass(ctx, tx, r.Cipher, s.Organisation, r.Clock.Now(), s.Member, id, kind, number, issued, expires)
		return err
	})
	return out, err
}

func (r RestrictedService) Passes(ctx context.Context, s api.Session, id uuid.UUID) ([]api.Pass, error) {
	var out []api.Pass
	err := r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if err := personExists(ctx, tx, id); err != nil {
			return err
		}
		rows, err := WorkPasses(ctx, tx, id)
		for _, p := range rows {
			out = append(out, api.Pass{ID: p.ID.String(), Type: p.Type, Number: p.Number, Issued: p.Issued.Format(time.DateOnly), Expires: p.Expires.Format(time.DateOnly)})
		}
		return err
	})
	return out, err
}

func (r RestrictedService) AddDependant(ctx context.Context, s api.Session, id uuid.UUID, d people.Dependant, from time.Time) (uuid.UUID, error) {
	var out uuid.UUID
	err := r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = AddDependant(ctx, tx, s.Organisation, r.Clock.Now(), s.Member, id, d, from)
		return err
	})
	return out, err
}

func (r RestrictedService) Dependants(ctx context.Context, s api.Session, id uuid.UUID) ([]api.Dependant, error) {
	var out []api.Dependant
	var refusal error
	err := r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		held, err := r.roles(ctx, tx, s)
		if err != nil {
			return err
		}
		rows, err := Dependants(ctx, tx, s.Organisation, r.Clock.Now(), s.Member, held, id)
		if errors.Is(err, ErrHidden) {
			refusal = err
			return nil
		}
		for _, d := range rows {
			out = append(out, api.Dependant{ID: d.ID.String(), Relationship: d.Relationship, BirthDate: dateStr(d.BirthDate), AdoptionIntent: d.AdoptionIntent, Citizenship: d.Citizenship,
				From: d.From.Format(time.DateOnly), To: dateStr(d.To)})
		}
		return err
	})
	if err == nil {
		err = refusal
	}
	return out, err
}

func (r RestrictedService) AddDocument(ctx context.Context, s api.Session, id uuid.UUID, typ, title, class, contentType string, content []byte, viewers []string) (uuid.UUID, error) {
	if r.Blobs == nil {
		return uuid.Nil, api.ErrUnavailable
	}
	known := false
	for _, t := range people.DocumentTypes {
		known = known || t == typ
	}
	if !known {
		return uuid.Nil, &people.Refusal{Code: people.CodeInvalid, Message: "The document type is not contract, kets, letter, pass_copy, certificate or other."}
	}
	for _, v := range viewers {
		if _, ok := authzRole(v); !ok {
			return uuid.Nil, &people.Refusal{Code: people.CodeInvalid, Message: "A viewer is a role such as hr_admin, or employee for the person themselves."}
		}
	}
	doc := uuid.Must(uuid.NewV7())
	key := "people/" + s.Organisation.String() + "/" + id.String() + "/" + doc.String()
	err := r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if err := personExists(ctx, tx, id); err != nil {
			return err
		}
		if err := r.Blobs.Create(ctx, key, bytes.NewReader(content)); err != nil {
			return err
		}
		return AddDocument(ctx, tx, s.Organisation, r.Clock.Now(), s.Member, id, doc, DocumentInput{Type: typ, Title: title, RetentionClass: class, BlobKey: key, ContentType: contentType,
			Size: int64(len(content)), SHA256: sha256.Sum256(content), Viewers: viewers})
	})
	return doc, err
}

func authzRole(v string) (authz.Role, bool) {
	for _, r := range authz.Roles {
		if string(r) == v {
			return r, true
		}
	}
	return "", false
}

func (r RestrictedService) own(ctx context.Context, tx pgx.Tx, s api.Session, person uuid.UUID) bool {
	var own bool
	_ = tx.QueryRow(ctx, "SELECT coalesce(member_id = $2, false) FROM nemura.person WHERE id = $1", person, s.Member).Scan(&own)
	return own
}

func (r RestrictedService) Documents(ctx context.Context, s api.Session, id uuid.UUID) ([]api.Document, error) {
	var out []api.Document
	err := r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		held, err := r.roles(ctx, tx, s)
		if err != nil {
			return err
		}
		rows, err := Documents(ctx, tx, id, held, r.own(ctx, tx, s, id))
		for _, d := range rows {
			out = append(out, api.Document{ID: d.ID.String(), Type: d.Type, Title: d.Title, RetentionClass: d.RetentionClass, ContentType: d.ContentType, Size: d.Size, Viewers: d.Viewers, Created: d.Created.UTC().Format(time.RFC3339)})
		}
		return err
	})
	return out, err
}

func (r RestrictedService) Download(ctx context.Context, s api.Session, id, doc uuid.UUID) (string, []byte, error) {
	if r.Blobs == nil {
		return "", nil, api.ErrUnavailable
	}
	var ct string
	var content []byte
	err := r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		held, err := r.roles(ctx, tx, s)
		if err != nil {
			return err
		}
		rows, err := Documents(ctx, tx, id, held, r.own(ctx, tx, s, id))
		if err != nil {
			return err
		}
		for _, d := range rows {
			if d.ID != doc {
				continue
			}
			rc, err := r.Blobs.Get(ctx, d.BlobKey)
			if err != nil {
				return err
			}
			defer func() { _ = rc.Close() }()
			if content, err = io.ReadAll(rc); err != nil {
				return err
			}
			ct = d.ContentType
			return Audit(ctx, tx, s.Organisation, r.Clock.Now(), user(s.Member), "nemura.restricted.read", audit.Object{Type: "person", ID: id},
				map[string]audit.Change{"capability": {nil, "person document"}, "level": {nil, "full"}}, "")
		}
		return &people.Refusal{Code: people.CodeNotFound, Message: "There is no such document, or you may not see it."}
	})
	return ct, content, err
}

func (r RestrictedService) ProposeBank(ctx context.Context, s api.Session, id uuid.UUID, account string) (uuid.UUID, error) {
	if r.Cipher == nil {
		return uuid.Nil, api.ErrUnavailable
	}
	var out uuid.UUID
	err := r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = ProposeBankChange(ctx, tx, r.Cipher, s.Organisation, r.Clock.Now(), s.Member, id, account)
		return err
	})
	return out, err
}

func (r RestrictedService) DecideBank(ctx context.Context, s api.Session, change uuid.UUID, approve bool, reason string) error {
	return r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		held, err := r.roles(ctx, tx, s)
		if err != nil {
			return err
		}
		return DecideBankChange(ctx, tx, s.Organisation, r.Clock.Now(), s.Member, held, change, approve, reason)
	})
}

func (r RestrictedService) ConfirmBank(ctx context.Context, s api.Session, change uuid.UUID, fresh bool) error {
	return r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return ConfirmBankChange(ctx, tx, s.Organisation, r.Clock.Now(), s.Member, change, fresh)
	})
}

func (r RestrictedService) Bank(ctx context.Context, s api.Session, change uuid.UUID) (api.BankChange, error) {
	var out api.BankChange
	err := r.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		v, err := BankChange(ctx, tx, change)
		if err != nil {
			return err
		}
		held, err := r.roles(ctx, tx, s)
		if err != nil {
			return err
		}
		may := r.own(ctx, tx, s, v.Person)
		for _, role := range held {
			may = may || role == authz.HRAdmin || role == authz.PayrollAdmin || role == authz.Owner
		}
		if !may {
			return &people.Refusal{Code: people.CodeNotFound, Message: "There is no such bank account change."}
		}
		out = api.BankChange{ID: v.ID.String(), Person: v.Person.String(), Masked: v.Masked, State: v.State}
		return nil
	})
	return out, err
}
