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
	"strings"
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

// Pricer prices the AI connections' models from their sources, in order (AI-82, AI-85): the provider's own list,
// Google's billing catalogue for Agent Platform, then LiteLLM's public price catalogue. Each LiteLLM download is kept
// with its digest, licence check and outcome, the document in the file store under its digest, and only a valid one
// prices anything. A model is priced only from its provider's own entry, exactly or, with an Owner's confirmation, after
// normalising; a rate that falls by more than half waits for an Owner's review; a model nothing prices stays unpriced,
// saying why; an Owner's price is never overwritten.
type Pricer struct {
	Store      storage.Store
	Catalogues storage.AIPriceCatalogues
	Blobs      ports.BlobStore
	Catalogue  ports.PriceCatalogue
	// Billing is Google's Cloud Billing Catalog for Agent Platform; nil where there is none, such as locally.
	Billing ports.BillingCatalog
	// Connectors are this release's connectors, whose manifests name each provider as the catalogues do.
	Connectors *connector.Catalogue
	Clock      ports.Clock
}

// sources are what a read prices from: LiteLLM's latest valid download, if any, and Agent Platform's SKUs, if read.
type sources struct {
	rec     *storage.AIPriceCatalogue
	cat     aiprice.Catalogue
	skus    []ports.BillingSKU
	skusErr error // why Google's catalogue could not be read
}

// billing reads Agent Platform's SKUs once for a read; with no catalogue configured, none.
func (p Pricer) billing(ctx context.Context, src *sources) {
	if p.Billing == nil {
		src.skusErr = errors.New("no billing catalogue is configured here")
		return
	}
	src.skus, src.skusErr = p.Billing.SKUs(ctx)
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

// Daily downloads LiteLLM's catalogue and prices every organisation's models from their sources (AI-82). A download
// that fails or is rejected changes no price from it: the latest valid one prices instead, and the providers' lists and
// Google's catalogue still do. The download's failure is still reported.
func (p Pricer) Daily(ctx context.Context) error {
	orgs, err := p.Store.Organisations(ctx)
	if err != nil {
		return err
	}
	var failed []error
	var src sources
	if p.Catalogue != nil {
		rec, parsed, err := p.Download(ctx)
		switch {
		case err == nil && rec.Outcome == storage.AIPriceCatalogueValid:
			src.rec, src.cat = &rec, parsed
		case err != nil && !errors.Is(err, ports.ErrNoCatalogue):
			failed = append(failed, err)
		}
	}
	if src.rec == nil && len(orgs) > 0 {
		if rec, cat, err := p.Latest(ctx, orgs[0]); err == nil {
			src.rec, src.cat = &rec, cat
		} else if !errors.Is(err, ports.ErrNotFound) {
			failed = append(failed, err)
		}
	}
	p.billing(ctx, &src)
	for _, org := range orgs {
		if err := p.price(ctx, org, uuid.Nil, src); err != nil {
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

// naming is how the catalogues name a connection's provider, from its connector's manifest; false for a self-hosted
// server or a provider they do not name.
func (p Pricer) naming(c storage.AIConnection) (connector.PriceNaming, bool) {
	if c.SelfHosted || p.Connectors == nil {
		return connector.PriceNaming{}, false
	}
	b, ok := p.Connectors.Get(c.Connector)
	if !ok || b.Manifest.Model == nil {
		return connector.PriceNaming{}, false
	}
	return b.Manifest.Model.Naming(c.Preset, c.Auth)
}

// location is where an Agent Platform connection processes its calls: its configured location, Singapore by default.
func location(c storage.AIConnection) string {
	var config struct {
		Location string `json:"location"`
	}
	if json.Unmarshal(c.Configuration, &config) != nil || config.Location == "" {
		return "asia-southeast1"
	}
	return config.Location
}

// price prices the models of every connection of org (or of only, when not uuid.Nil) from src.
func (p Pricer) price(ctx context.Context, org, only uuid.UUID, src sources) error {
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
			if err := p.priceConnection(ctx, u, c, src, now); err != nil {
				return fmt.Errorf("%s: %w", c.ID, err)
			}
		}
		return nil
	})
}

func (p Pricer) priceConnection(ctx context.Context, u storage.UnitOfWork, c storage.AIConnection, src sources, now time.Time) error {
	models, err := u.AIModels().Of(ctx, c.ID)
	if err != nil {
		return err
	}
	current, err := u.AIPrices().Current(ctx, c.ID)
	if err != nil {
		return err
	}
	versions := map[string]map[string]storage.AIPrice{aiprice.Active: {}, aiprice.Pending: {}, aiprice.Declined: {}}
	for _, v := range current {
		if by, ok := versions[v.Status]; ok {
			by[v.Model] = v
		}
	}
	for _, m := range models {
		if !m.Listed {
			continue // no longer listed: it serves nothing, and keeps the price it had
		}
		why, next := p.decide(c, m, versions, src, now)
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

// find is the first source that prices a model, in AI-82's order, with its source; or why none does.
func (p Pricer) find(c storage.AIConnection, m storage.AIModel, src sources) (aiprice.Found, string, string) {
	naming, named := p.naming(c)
	var notes []string
	// (a) The provider's own list.
	if lp := listedPriceOf(m.ListedPrice); lp != nil {
		price, err := aiprice.FromList(*lp)
		if err == nil {
			return aiprice.Found{Entry: &aiprice.Entry{Key: m.Model, Provider: aiprice.SourceProviderList, Price: price}, Match: aiprice.MatchExact},
				aiprice.SourceProviderList, ""
		}
		notes = append(notes, err.Error())
	}
	// (b) Google's billing catalogue, for Agent Platform.
	if named && naming.GoogleBilling {
		if src.skusErr != nil {
			notes = append(notes, "Google's billing catalogue could not be read: "+src.skusErr.Error())
		} else if f := aiprice.FromBilling(src.skus, location(c), m.Name); f.Problem == "" {
			return f, aiprice.SourceGoogleBilling, ""
		} else {
			notes = append(notes, f.Problem)
		}
	}
	// (c) LiteLLM's public price catalogue.
	switch {
	case !named:
		notes = append(notes, "the price catalogue does not name this connection's provider")
	case src.rec == nil:
		notes = append(notes, "there is no valid download of LiteLLM's price catalogue yet")
	default:
		f := src.cat.Find(aiprice.Naming{Provider: naming.Provider, Prefix: naming.Prefix}, m.Model)
		if f.Problem == "" {
			return f, aiprice.SourceLiteLLM, ""
		}
		notes = append(notes, f.Problem)
	}
	return aiprice.Found{}, "", strings.Join(notes, "; ")
}

// decide is what a read makes of one model: the version to record, if any, and why it has no new price ("" when it
// has, or needs none).
func (p Pricer) decide(c storage.AIConnection, m storage.AIModel, versions map[string]map[string]storage.AIPrice, src sources,
	now time.Time) (string, *storage.AIPrice) {
	was, inForce := versions[aiprice.Active][m.Model]
	switch {
	case inForce && was.Source == aiprice.SourceOwner:
		return "", nil // an Owner's price is never overwritten by a read (AI-82)
	case c.SelfHosted:
		return "a self-hosted server needs no per-token price", nil
	case inForce && was.Source == aiprice.SourceGoogleBilling && src.skusErr != nil:
		// The source in force could not be read this time: its price stays, rather than the next source's.
		return "Google's billing catalogue could not be read: " + src.skusErr.Error() + "; the price in force is kept", nil
	}
	f, source, why := p.find(c, m, src)
	if f.Entry == nil {
		if inForce {
			return why + "; the price in force is kept", nil
		}
		return why, nil
	}
	next := storedPrice(f.Entry.Price)
	next.ConnectionID, next.Model, next.Source, next.CatalogueKey, next.Match = c.ID, m.Model, source, f.Entry.Key, f.Match
	if source == aiprice.SourceLiteLLM {
		next.CatalogueID = &src.rec.ID
	}
	next.RecordedBy, next.RecordedAt, next.Status = priceBy, now, aiprice.Active
	// The same price from the same entry as the one in force, the one waiting, or the one the Owner last declined:
	// nothing to record.
	for _, v := range []storage.AIPrice{was, versions[aiprice.Pending][m.Model], versions[aiprice.Declined][m.Model]} {
		if v.ID != uuid.Nil && v.Source == source && v.CatalogueKey == next.CatalogueKey && aiprice.Same(domainPrice(v), f.Entry.Price) {
			return "", nil
		}
	}
	switch {
	case f.Match == aiprice.MatchNormalised:
		next.Status, next.Review = aiprice.Pending, aiprice.ReviewNormalised
		next.ReviewDetail = "matched " + f.Entry.Key + " of the " + source + " source only by its name, not exactly"
		if source == aiprice.SourceLiteLLM {
			next.ReviewDetail = "matched the catalogue's " + f.Entry.Key + " only after removing a provider prefix or a version date"
		}
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

// PriceItem is the outbox item that prices a connection's models from their sources, once for the change that asked
// for it: a read of its list that added models or gave prices, or an Owner's price withdrawn.
func PriceItem(connection, change uuid.UUID) (storage.NewOutboxItem, error) {
	payload, err := json.Marshal(map[string]string{"connection_id": connection.String()})
	return storage.NewOutboxItem{Class: storage.ClassWorker, Kind: PriceKind, Payload: payload,
		BusinessKey: "ai-prices:" + connection.String() + ":" + change.String()}, err
}

// Work is the outbox's work of PriceKind: price the connection its payload names from its sources, LiteLLM's latest
// valid download among them when there is one.
func (p Pricer) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var in struct {
			Connection uuid.UUID `json:"connection_id"`
		}
		if err := json.Unmarshal(row.Payload, &in); err != nil {
			return fmt.Errorf("%s: %w", PriceKind, err)
		}
		var src sources
		rec, cat, err := p.Latest(ctx, org)
		switch {
		case err == nil:
			src.rec, src.cat = &rec, cat
		case !errors.Is(err, ports.ErrNotFound):
			return err
		}
		p.billing(ctx, &src)
		return p.price(ctx, org, in.Connection, src)
	}
}
