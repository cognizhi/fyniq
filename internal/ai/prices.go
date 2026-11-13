package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/aiprice"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// PriceJob is the platform job that reads the price catalogue and prices every organisation's models, daily at 06:15
// Singapore time, after the model lists (AI-82, AI-85).
const PriceJob = "ai.prices.read"

// PriceKind is the outbox's work of pricing one connection's models from the latest valid catalogue, as after a read
// of its list added models.
const PriceKind = "ai.prices.read"

// priceBy is who records the prices a read finds: the system user (AI-82).
const priceBy = "system:" + PriceJob

// Pricer reads LiteLLM's public price catalogue and prices the AI connections' models from it (AI-82, AI-85): each
// download is kept with its digest, licence check and outcome, the document in the file store under its digest, and
// only a valid one prices anything. A model is priced only from an entry under its provider's name, exactly or, with an
// Owner's confirmation, after normalising; a rate that falls by more than half waits for an Owner's review; a model
// nothing prices stays unpriced, saying why; an Owner's price is never overwritten.
type Pricer struct {
	Store      storage.Store
	Catalogues storage.AIPriceCatalogues
	Blobs      ports.BlobStore
	Catalogue  ports.PriceCatalogue
	// Connectors are this release's connectors, whose manifests name each provider as the catalogue does.
	Connectors *connector.Catalogue
	Clock      ports.Clock
}

// documentKey is where a download is kept in the file store: under its digest, so the same document is kept once.
func documentKey(source, digest string) string {
	return "ai/price-catalogues/" + source + "/" + digest + ".json"
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Download fetches the catalogue, checks its licence and reads it, keeps the document and records the download: valid,
// with the catalogue it read, or rejected with why (AI-85). ports.ErrNoCatalogue when none is configured.
func (p Pricer) Download(ctx context.Context) (storage.AIPriceCatalogue, aiprice.Catalogue, error) {
	doc, err := p.Catalogue.Fetch(ctx)
	if err != nil {
		return storage.AIPriceCatalogue{}, aiprice.Catalogue{}, err
	}
	digest := digestOf(doc.Body)
	rec := storage.AIPriceCatalogue{Source: doc.Source, URL: doc.URL, FetchedAt: p.Clock.Now(), SHA256: digest, Size: int64(len(doc.Body)),
		LicenceURL: doc.LicenceURL, LicenceSHA256: digestOf(doc.Licence), Outcome: storage.AIPriceCatalogueValid,
		Document: documentKey(doc.Source, digest)}
	var parsed aiprice.Catalogue
	licence, err := aiprice.CheckLicence(doc.Licence, doc.Path)
	if err == nil {
		rec.Licence = licence
		parsed, err = aiprice.ParseLiteLLM(doc.Body)
	}
	if err != nil {
		rec.Outcome, rec.Failure = storage.AIPriceCatalogueRejected, cut(err.Error())
	} else {
		rec.Entries = parsed.Entries
	}
	if err := p.Blobs.Create(ctx, rec.Document, bytes.NewReader(doc.Body)); err != nil && !errors.Is(err, ports.ErrExists) {
		return storage.AIPriceCatalogue{}, aiprice.Catalogue{}, fmt.Errorf("keeping the price catalogue: %w", err)
	}
	rec, err = p.Catalogues.RecordAIPriceCatalogue(ctx, rec)
	return rec, parsed, err
}

func cut(s string) string {
	if len(s) > 500 {
		return s[:497] + "…"
	}
	return s
}

// Daily downloads the catalogue and, when it is valid, prices every organisation's models from it (AI-82). A
// rejected download changes no price: the ones in force stay.
func (p Pricer) Daily(ctx context.Context) error {
	if p.Catalogue == nil {
		return nil // none configured: every model stays unpriced, as with no source
	}
	rec, parsed, err := p.Download(ctx)
	if errors.Is(err, ports.ErrNoCatalogue) {
		return nil // none configured: every model stays unpriced, as with no source
	}
	if err != nil || rec.Outcome != storage.AIPriceCatalogueValid {
		return err
	}
	orgs, err := p.Store.Organisations(ctx)
	if err != nil {
		return err
	}
	var failed []error
	for _, org := range orgs {
		if err := p.Price(ctx, org, uuid.Nil, rec, parsed); err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", org, err))
		}
	}
	return errors.Join(failed...)
}

// Latest is the newest valid download, read again from the file store and checked against its recorded digest, so a
// document changed after its download is never used. ports.ErrNotFound when there is none yet.
func (p Pricer) Latest(ctx context.Context, org uuid.UUID) (storage.AIPriceCatalogue, aiprice.Catalogue, error) {
	var rec storage.AIPriceCatalogue
	if err := p.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		rec, err = u.AIPrices().LatestCatalogue(ctx, aiprice.SourceLiteLLM)
		return err
	}); err != nil {
		return rec, aiprice.Catalogue{}, err
	}
	r, err := p.Blobs.Get(ctx, rec.Document)
	if err != nil {
		return rec, aiprice.Catalogue{}, fmt.Errorf("the price catalogue %s: %w", rec.ID, err)
	}
	defer func() { _ = r.Close() }()
	body, err := io.ReadAll(io.LimitReader(r, aiprice.MaxSize+1))
	if err != nil {
		return rec, aiprice.Catalogue{}, err
	}
	if digestOf(body) != rec.SHA256 {
		return rec, aiprice.Catalogue{}, fmt.Errorf("the price catalogue %s no longer matches its digest: it is not used", rec.ID)
	}
	parsed, err := aiprice.ParseLiteLLM(body)
	return rec, parsed, err
}

// naming is how the catalogue names a connection's provider, from its connector's manifest; false for a self-hosted
// server or a provider the catalogue does not name.
func (p Pricer) naming(c storage.AIConnection) (aiprice.Naming, bool) {
	if c.SelfHosted || p.Connectors == nil {
		return aiprice.Naming{}, false
	}
	b, ok := p.Connectors.Get(c.Connector)
	if !ok || b.Manifest.Model == nil {
		return aiprice.Naming{}, false
	}
	n, ok := b.Manifest.Model.Naming(c.Preset, c.Auth)
	return aiprice.Naming{Provider: n.Provider, Prefix: n.Prefix}, ok
}

// Price prices the models of every connection of org (or of only, when not uuid.Nil) from a valid catalogue.
func (p Pricer) Price(ctx context.Context, org, only uuid.UUID, rec storage.AIPriceCatalogue, cat aiprice.Catalogue) error {
	now := p.Clock.Now()
	return p.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.AIConnections().List(ctx)
		if err != nil {
			return err
		}
		for _, c := range all {
			if only != uuid.Nil && c.ID != only {
				continue
			}
			if err := p.priceConnection(ctx, u, c, rec, cat, now); err != nil {
				return fmt.Errorf("%s: %w", c.ID, err)
			}
		}
		return nil
	})
}

func (p Pricer) priceConnection(ctx context.Context, u storage.UnitOfWork, c storage.AIConnection, rec storage.AIPriceCatalogue,
	cat aiprice.Catalogue, now time.Time) error {
	models, err := u.AIModels().Of(ctx, c.ID)
	if err != nil {
		return err
	}
	current, err := u.AIPrices().Current(ctx, c.ID)
	if err != nil {
		return err
	}
	active, pending, declined := map[string]storage.AIPrice{}, map[string]storage.AIPrice{}, map[string]storage.AIPrice{}
	for _, v := range current {
		switch v.Status {
		case aiprice.Active:
			active[v.Model] = v
		case aiprice.Pending:
			pending[v.Model] = v
		case aiprice.Declined:
			declined[v.Model] = v
		}
	}
	naming, named := p.naming(c)
	for _, m := range models {
		if !m.Listed {
			continue // no longer listed: it serves nothing, and keeps the price it had
		}
		why, next := p.decide(c, m.Model, naming, named, active, pending, declined, rec, cat, now)
		if next != nil {
			if _, err := u.AIPrices().Record(ctx, *next); err != nil {
				return fmt.Errorf("%s: %w", m.Model, err)
			}
		}
		if err := u.AIPrices().Checked(ctx, c.ID, m.Model, why, now); err != nil {
			return fmt.Errorf("%s: %w", m.Model, err)
		}
	}
	return nil
}

// decide is what a read makes of one model: the version to record, if any, and why it has no new price ("" when it
// has, or needs none).
func (p Pricer) decide(c storage.AIConnection, model string, naming aiprice.Naming, named bool, active, pending, declined map[string]storage.AIPrice,
	rec storage.AIPriceCatalogue, cat aiprice.Catalogue, now time.Time) (string, *storage.AIPrice) {
	was, inForce := active[model]
	switch {
	case inForce && was.Source == aiprice.SourceOwner:
		return "", nil // an Owner's price is never overwritten by a read (AI-82)
	case c.SelfHosted:
		return "a self-hosted server needs no per-token price", nil
	case !named:
		return "the price catalogue does not name this connection's provider", nil
	}
	f := cat.Find(naming, model)
	if f.Problem != "" {
		if inForce {
			return f.Problem + "; the price in force is kept", nil
		}
		return f.Problem, nil
	}
	next := storedPrice(f.Entry.Price)
	next.ConnectionID, next.Model, next.Source, next.CatalogueID, next.CatalogueKey, next.Match = c.ID, model, aiprice.SourceLiteLLM,
		&rec.ID, f.Entry.Key, f.Match
	next.RecordedBy, next.RecordedAt, next.Status = priceBy, now, aiprice.Active
	// The same price as the one in force, the one waiting, or the one the Owner last declined: nothing to record.
	for _, v := range []storage.AIPrice{was, pending[model], declined[model]} {
		if v.ID != uuid.Nil && v.CatalogueKey == next.CatalogueKey && aiprice.Same(domainPrice(v), f.Entry.Price) {
			return "", nil
		}
	}
	switch {
	case f.Match == aiprice.MatchNormalised:
		next.Status, next.Review = aiprice.Pending, aiprice.ReviewNormalised
		next.ReviewDetail = "matched the catalogue's " + f.Entry.Key + " only after removing a provider prefix or a version date"
	case inForce:
		if drop := aiprice.Drop(domainPrice(was), f.Entry.Price); drop != "" {
			next.Status, next.Review, next.ReviewDetail = aiprice.Pending, aiprice.ReviewDrop, drop
		}
	}
	return "", &next
}

// storedPrice is a price as the store keeps it: each rate as exact decimal text.
func storedPrice(p aiprice.Price) storage.AIPrice {
	out := storage.AIPrice{Currency: p.Currency, Ignored: p.Ignored}
	for _, r := range p.Rates {
		out.Rates = append(out.Rates, storage.AIPriceRate{Kind: r.Kind, Above: r.Above, Unit: r.Unit(), Amount: r.Amount.Text('f')})
	}
	return out
}

// domainPrice is a stored price as the domain compares and computes with it.
func domainPrice(v storage.AIPrice) aiprice.Price {
	out := aiprice.Price{Currency: v.Currency, Ignored: v.Ignored}
	for _, r := range v.Rates {
		d, _, err := apd.NewFromString(aiprice.Trim(r.Amount))
		if err != nil {
			continue // the database keeps NUMERIC: never reached
		}
		out.Rates = append(out.Rates, aiprice.Rate{Kind: r.Kind, Above: r.Above, Amount: *d})
	}
	return out
}

// PriceItem is the outbox item that prices a connection's models from the latest valid catalogue, once for the read
// of its list that added models.
func PriceItem(connection, read uuid.UUID) (storage.NewOutboxItem, error) {
	payload, err := json.Marshal(map[string]string{"connection_id": connection.String()})
	return storage.NewOutboxItem{Class: storage.ClassWorker, Kind: PriceKind, Payload: payload,
		BusinessKey: "ai-prices:" + connection.String() + ":" + read.String()}, err
}

// Work is the outbox's work of PriceKind: price the connection its payload names from the latest valid catalogue; with
// none yet, the daily read prices it.
func (p Pricer) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var in struct {
			Connection uuid.UUID `json:"connection_id"`
		}
		if err := json.Unmarshal(row.Payload, &in); err != nil {
			return fmt.Errorf("%s: %w", PriceKind, err)
		}
		rec, cat, err := p.Latest(ctx, org)
		if errors.Is(err, ports.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return p.Price(ctx, org, in.Connection, rec, cat)
	}
}
