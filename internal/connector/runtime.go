package connector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Runtime builds, for one piece of work, the adapters of the connectors enabled when it starts (FR-INT-20, G4-096):
// read in the work's own unit of work, never cached, so a pause or a disable stops the next piece of work on every
// instance; their secrets through the cache, by pinned reference; their traffic through the egress guard, to the
// destinations they consented to only (SEC-36, FR-INT-26); each behind its manifest's rate limit and circuit breaker
// (FR-INT-27).
type Runtime struct {
	Catalogue    *Catalogue
	Secrets      *SecretCache
	Egress       *egress.Guard
	SenderDomain string
	Clock        ports.Clock

	mu     sync.Mutex
	guards map[uuid.UUID]*guard
}

// Authority is what makes a capability act, checked against its manifest's trigger (FR-INT-27): a system rule (such as a
// message the outbox holds), or a completed human authority, named by its approval. Work started from an AI proposal
// still needs the human authority of the underlying action: a connector never approves anything.
type Authority struct {
	Trigger  string    // TriggerSystem or TriggerHuman
	Rule     string    // for a system rule: which
	Approval uuid.UUID // for a human authority: the approval that completed it
	FromAI   bool      // the work comes from an AI proposal
}

// OutboxEmail is the authority of a message the outbox holds for sending.
var OutboxEmail = Authority{Trigger: TriggerSystem, Rule: "a message the outbox holds"}

// ErrTrigger is work whose authority is not what the capability's manifest names.
var ErrTrigger = errors.New("the work's authority is not what the connector's manifest allows")

func checkTrigger(m Manifest, k Kind, a Authority) error {
	c, ok := m.Capability(k)
	switch {
	case !ok:
		return fmt.Errorf("%w: %s does not implement %s", ErrTrigger, m.ID, k)
	case c.Trigger == TriggerNone:
		return fmt.Errorf("%w: nothing may make %s's %s act", ErrTrigger, m.ID, k)
	case a.Trigger != c.Trigger:
		return fmt.Errorf("%w: %s's %s acts on a %s, not a %s", ErrTrigger, m.ID, k, c.Trigger, a.Trigger)
	case c.Trigger == TriggerHuman && a.Approval == uuid.Nil:
		return fmt.Errorf("%w: %s's %s needs the approval that completed its human authority", ErrTrigger, m.ID, k)
	case a.FromAI && c.Trigger != TriggerHuman:
		return fmt.Errorf("%w: work from an AI proposal needs the human authority of the underlying action", ErrTrigger)
	}
	return nil
}

// Mail is the outbound email of one piece of work.
type Mail struct {
	// Enabled are the enabled connectors' adapters, the primary first; none means nothing may send now, and the work
	// waits.
	Enabled []ports.Email
	// Reconcile has every installed connector's adapter by name, enabled, paused or disabled: an earlier attempt is
	// reconciled with the provider it went to, never dropped (FR-INT-26: in-flight work finishes or is reconciled).
	Reconcile map[string]ports.Email
	// Skipped says why an enabled connector could not be used now, such as a secret that could not be read.
	Skipped []string
}

// OutboundEmail is the outbound email of a piece of work under authority a, read in its unit of work.
func (r *Runtime) OutboundEmail(ctx context.Context, u storage.UnitOfWork, a Authority) (Mail, error) {
	all, err := u.Connectors().Installation(ctx)
	if err != nil {
		return Mail{}, err
	}
	secrets, err := u.Connectors().InstallationSecrets(ctx)
	if err != nil {
		return Mail{}, err
	}
	return r.Mail(ctx, all, secrets, a)
}

// Mail builds the outbound email from the installed connectors and their secret versions, as read at the work's start.
func (r *Runtime) Mail(ctx context.Context, all []storage.InstalledConnector, secrets []storage.ConnectorSecret, a Authority) (Mail, error) {
	out := Mail{Reconcile: map[string]ports.Email{}}
	ordered := slices.Clone(all)
	slices.SortStableFunc(ordered, func(x, y storage.InstalledConnector) int { return x.Position - y.Position })
	for _, c := range ordered {
		if c.State != storage.ConnectorEnabled && c.State != storage.ConnectorPaused && c.State != storage.ConnectorDisabled {
			continue
		}
		b, ok := r.Catalogue.Get(c.Connector)
		if !ok || b.OutboundEmail == nil {
			continue
		}
		if err := checkTrigger(b.Manifest, OutboundEmail, a); err != nil {
			return Mail{}, err
		}
		config, err := r.config(ctx, c, secrets)
		if err != nil {
			out.Skipped = append(out.Skipped, c.Connector+": "+err.Error())
			continue
		}
		e, err := b.OutboundEmail(config)
		if err != nil {
			out.Skipped = append(out.Skipped, c.Connector+": "+err.Error())
			continue
		}
		e = guardedEmail{Email: e, g: r.guard(c.ID, b.Manifest.Limits), clock: r.Clock}
		out.Reconcile[e.Name()] = e
		if c.State == storage.ConnectorEnabled {
			out.Enabled = append(out.Enabled, e)
		}
	}
	return out, nil
}

// InboundEmail is the first enabled connector that receives email, and its record; ports.ErrNotFound when none is.
func (r *Runtime) InboundEmail(ctx context.Context, u storage.UnitOfWork, a Authority) (ports.InboundMail, storage.InstalledConnector, error) {
	all, err := u.Connectors().Installation(ctx)
	if err != nil {
		return nil, storage.InstalledConnector{}, err
	}
	secrets, err := u.Connectors().InstallationSecrets(ctx)
	if err != nil {
		return nil, storage.InstalledConnector{}, err
	}
	slices.SortStableFunc(all, func(x, y storage.InstalledConnector) int { return x.Position - y.Position })
	for _, c := range all {
		b, ok := r.Catalogue.Get(c.Connector)
		if c.State != storage.ConnectorEnabled || !ok || b.InboundEmail == nil {
			continue
		}
		if err := checkTrigger(b.Manifest, InboundEmail, a); err != nil {
			return nil, c, err
		}
		config, err := r.config(ctx, c, secrets)
		if err != nil {
			return nil, c, err
		}
		in, err := b.InboundEmail(config)
		return in, c, err
	}
	return nil, storage.InstalledConnector{}, ports.ErrNotFound
}

// config is what connector c's binding builds from: its configuration, its current secrets, and clients bound to its
// consented destinations.
func (r *Runtime) config(ctx context.Context, c storage.InstalledConnector, secrets []storage.ConnectorSecret) (Config, error) {
	values := map[string]string{}
	for _, s := range secrets {
		if s.ConnectorID == c.ID && s.ValidUntil == nil && s.DestroyedAt == nil {
			v, err := r.Secrets.Get(ctx, s.Ref)
			if err != nil {
				return Config{}, fmt.Errorf("its %s cannot be read: %w", s.Name, err)
			}
			values[s.Name] = v
		}
	}
	allow := Allows(c.Consent, c.Configuration)
	g := r.Egress
	return Config{Values: c.Configuration, Secrets: values, SenderDomain: r.SenderDomain, Consent: c.Consent,
		HTTP: g.Client(allow),
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", egress.ErrRefused, err)
			}
			p, _ := strconv.Atoi(port)
			if !allow(strings.ToLower(host), p) {
				return nil, fmt.Errorf("%w: %s is not one of the connector's destinations", egress.ErrRefused, host)
			}
			return g.DialContext(ctx, network, addr)
		}}, nil
}

// Allows is whether a connector that consented to consent, configured with config, may reach host and port: a host
// it declared (with its subdomains for "*."), or the host a configuration property it declared names.
func Allows(consent storage.ConnectorConsent, config map[string]any) func(host string, port int) bool {
	return func(host string, port int) bool {
		host = strings.ToLower(strings.TrimSuffix(host, "."))
		for _, d := range consent.Destinations {
			if d.Port != 0 && d.Port != port {
				continue
			}
			name := d.Host
			if d.Configured != "" {
				name = configuredHost(config[d.Configured])
			}
			switch {
			case name == "":
			case strings.HasPrefix(name, "*."):
				if strings.HasSuffix(host, name[1:]) {
					return true
				}
			case host == name:
				return true
			}
		}
		return false
	}
}

// configuredHost is the host a configuration value names: a URL's, a host:port's, or a bare host.
func configuredHost(v any) string {
	s, _ := v.(string)
	if s == "" {
		return ""
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		return strings.ToLower(u.Hostname())
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(s)
}

// ------------------------------------------------------------------ the connection test

// TestEmail is the email connectors' test action (FR-INT-20): one test message to the person's own verified address,
// through connector c even before it is enabled, and a result in Fyniq's words, never what the provider answered
// (SEC-36).
func (r *Runtime) TestEmail(ctx context.Context, c storage.InstalledConnector, secrets []storage.ConnectorSecret, to, from string) storage.ConnectorTest {
	now := r.Clock.Now()
	fail := func(reason string) storage.ConnectorTest {
		return storage.ConnectorTest{Passed: false, Reason: reason, At: now}
	}
	b, ok := r.Catalogue.Get(c.Connector)
	if !ok || b.OutboundEmail == nil {
		return fail("This connector does not send email.")
	}
	config, err := r.config(ctx, c, secrets)
	if err != nil {
		return fail("A secret of the connector cannot be read: set it again.")
	}
	e, err := b.OutboundEmail(config)
	if err != nil {
		return fail("The connector is not fully set up: " + setupReason(err))
	}
	m := ports.OutboundEmail{Key: "test-" + uuid.NewString(), Reference: "test", From: from, To: []string{to},
		Subject: "Fyniq connection test", Text: "This message tests the email connector " + b.Manifest.Name + ". Nothing else happens.\n", Date: now}
	_, err = e.Send(ctx, m)
	switch {
	case err == nil:
		return storage.ConnectorTest{Passed: true, Reason: b.Manifest.Name + " accepted the test message.", At: now}
	case errors.Is(err, egress.ErrRefused), errors.Is(err, egress.ErrRedirect):
		return fail("Fyniq refused the connection: the address is not one the connector may reach (a private or internal address, or a redirect).")
	case errors.Is(err, ports.ErrEmailRefused):
		return fail(b.Manifest.Name + " refused the test message: check the key, the endpoint and that the sending domain is verified there.")
	}
	return fail(b.Manifest.Name + " did not answer in time, or the answer was lost: try again.")
}

// setupReason is a binding's own refusal, which names what to set and never a value.
func setupReason(err error) string {
	s := err.Error()
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// ------------------------------------------------------------------ the principal

// Principal is the context a connector's work is audited under (FR-INT-27): the connector as its own principal, on
// behalf of the person or rule that made it act.
func Principal(ctx context.Context, c storage.InstalledConnector, onBehalfOf *storage.Actor) context.Context {
	a := storage.AuditFrom(ctx)
	a.Actor = &storage.Actor{Type: "integration", ID: "connector/" + c.ID.String()}
	if onBehalfOf != nil {
		a.OnBehalfOf = onBehalfOf
	}
	return storage.WithAudit(ctx, a)
}

// ------------------------------------------------------------------ the manifest diff

// ConsentDiff is what a release's manifest asks beyond, and no longer asks of, what a connector consented to.
type ConsentDiff struct {
	Added, Removed storage.ConnectorConsent
}

// Widens is whether the release's manifest asks for more: then it runs under the old consent until someone consents
// again (FR-INT-26).
func (d ConsentDiff) Widens() bool {
	a := d.Added
	return len(a.DataClasses)+len(a.ProviderScopes)+len(a.FyniqScopes)+len(a.Destinations)+len(a.Webhooks) > 0
}

// Diff compares what a connector consented to with the release's manifest m.
func Diff(consented storage.ConnectorConsent, m Manifest) ConsentDiff {
	now := m.Consent()
	var d ConsentDiff
	d.Added.DataClasses, d.Removed.DataClasses = minus(now.DataClasses, consented.DataClasses), minus(consented.DataClasses, now.DataClasses)
	d.Added.ProviderScopes, d.Removed.ProviderScopes = minus(now.ProviderScopes, consented.ProviderScopes), minus(consented.ProviderScopes, now.ProviderScopes)
	d.Added.FyniqScopes, d.Removed.FyniqScopes = minus(now.FyniqScopes, consented.FyniqScopes), minus(consented.FyniqScopes, now.FyniqScopes)
	d.Added.Webhooks, d.Removed.Webhooks = minus(now.Webhooks, consented.Webhooks), minus(consented.Webhooks, now.Webhooks)
	d.Added.Destinations, d.Removed.Destinations = minus(now.Destinations, consented.Destinations), minus(consented.Destinations, now.Destinations)
	return d
}

func minus[T comparable](a, b []T) []T {
	var out []T
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

// ------------------------------------------------------------------ rate limit and circuit breaker

// guard is one installed connector's rate limit (a token bucket) and circuit breaker, per instance (FR-INT-27).
type guard struct {
	mu        sync.Mutex
	limits    Limits
	tokens    float64
	last      time.Time
	failures  int
	openUntil time.Time
	trial     bool
}

func (r *Runtime) guard(id uuid.UUID, l Limits) *guard {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.guards == nil {
		r.guards = map[uuid.UUID]*guard{}
	}
	g, ok := r.guards[id]
	if !ok || g.limits != l {
		g = &guard{limits: l, tokens: float64(l.RequestsPerMinute)}
		r.guards[id] = g
	}
	return g
}

// ErrHeld is work a connector's limit or open breaker kept from starting: nothing was handed over, so it is safe to
// send elsewhere or later.
var ErrHeld = errors.New("held by the connector's rate limit or circuit breaker")

// begin takes a token and checks the breaker at now.
func (g *guard) begin(now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Before(g.openUntil) {
		return fmt.Errorf("%w: the circuit breaker is open until %s", ErrHeld, g.openUntil.UTC().Format(time.RFC3339))
	}
	if !g.openUntil.IsZero() { // the cool-down ended: one trial
		if g.trial {
			return fmt.Errorf("%w: a trial after the circuit breaker opened is running", ErrHeld)
		}
		g.trial = true
	}
	per := float64(g.limits.RequestsPerMinute)
	if !g.last.IsZero() {
		g.tokens = min(per, g.tokens+now.Sub(g.last).Minutes()*per)
	}
	g.last = now
	if g.tokens < 1 {
		return fmt.Errorf("%w: %d requests a minute", ErrHeld, g.limits.RequestsPerMinute)
	}
	g.tokens--
	return nil
}

// end records how the work ended: a transport failure (the provider never answered clearly) counts toward the breaker;
// an answer, accepted or refused, closes it.
func (g *guard) end(failed bool, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.trial = false
	if !failed {
		g.failures, g.openUntil = 0, time.Time{}
		return
	}
	g.failures++
	if g.failures >= g.limits.BreakerFailures {
		g.openUntil = now.Add(time.Duration(g.limits.BreakerCooldownSeconds) * time.Second)
	}
}

// guardedEmail is an email adapter behind its connector's guard. A send the guard holds is a refusal: it was never
// handed over, so the failover may send it (FR-INT-21). Status, the reconciliation of an earlier attempt, is never
// held: in-flight work is reconciled whatever the breaker says.
type guardedEmail struct {
	ports.Email
	g     *guard
	clock ports.Clock
}

func (e guardedEmail) Send(ctx context.Context, m ports.OutboundEmail) (ports.EmailReceipt, error) {
	if err := e.g.begin(e.clock.Now()); err != nil {
		return ports.EmailReceipt{}, fmt.Errorf("%w: %w", ports.ErrEmailRefused, err)
	}
	r, err := e.Email.Send(ctx, m)
	e.g.end(err != nil && !errors.Is(err, ports.ErrEmailRefused), e.clock.Now())
	return r, err
}
