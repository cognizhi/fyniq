package connector

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Config is what a binding builds a connector's adapter from, for one piece of work: the configuration the manifest's
// schema accepted, the secrets resolved for this work only, and the HTTP client its traffic goes through.
type Config struct {
	Values  map[string]any
	Secrets map[string]string
	// HTTP is the client for the connector's HTTP traffic; nil leaves the adapter's own. The runtime gives the egress
	// guard's client, bound to the manifest's consented destinations (SEC-36).
	HTTP *http.Client
	// Dial connects a connector that is not HTTP (SMTP); nil dials directly. The runtime gives the egress guard's, bound
	// to the consented destinations.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// SenderDomain is the platform sender's domain, for an adapter that needs one (SMTP's Message-ID).
	SenderDomain string
	// Consent is what the installed connector consented to send out and reach; a binding leaves off what needs more.
	Consent storage.ConnectorConsent
}

// String returns the configuration value name as a string, or def when it is unset or empty.
func (c Config) String(name, def string) string {
	if v, ok := c.Values[name].(string); ok && v != "" {
		return v
	}
	return def
}

// Secret returns the secret name, or an error naming what to set when it is not set.
func (c Config) Secret(name string) (string, error) {
	if v := c.Secrets[name]; v != "" {
		return v, nil
	}
	return "", fmt.Errorf("the secret %s is not set: set it in Settings › Integrations", name)
}

// Binding ties a manifest to the adapters that implement its capabilities. A binding builds a capability's adapter
// only when the manifest lists that capability, and has a builder for every capability the manifest lists.
type Binding struct {
	Manifest      Manifest
	OutboundEmail func(Config) (ports.Email, error)
	InboundEmail  func(Config) (ports.InboundMail, error)
}

// kinds are the capabilities the binding has builders for.
func (b Binding) kinds() []Kind {
	var ks []Kind
	if b.OutboundEmail != nil {
		ks = append(ks, OutboundEmail)
	}
	if b.InboundEmail != nil {
		ks = append(ks, InboundEmail)
	}
	return ks
}

// Catalogue is the connectors compiled into this release (FR-INT-25).
type Catalogue struct {
	bindings []Binding
}

// NewCatalogue checks and orders the bindings: each id once, each manifest's capabilities exactly the builders its
// binding has.
func NewCatalogue(bs ...Binding) (*Catalogue, error) {
	var problems []string
	var ids []string
	for _, b := range bs {
		m := b.Manifest
		if m.hash == "" {
			problems = append(problems, fmt.Sprintf("%s: the manifest was not parsed", m.ID))
			continue
		}
		if slices.Contains(ids, m.ID) {
			problems = append(problems, fmt.Sprintf("%s is in the catalogue twice", m.ID))
		}
		ids = append(ids, m.ID)
		var declared []Kind
		for _, c := range m.Capabilities {
			declared = append(declared, c.Kind)
		}
		built := b.kinds()
		slices.Sort(declared)
		slices.Sort(built)
		if !slices.Equal(declared, built) {
			problems = append(problems, fmt.Sprintf("%s: the manifest declares %v but the binding builds %v", m.ID, declared, built))
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("connector catalogue: %s", strings.Join(problems, "; "))
	}
	sorted := slices.Clone(bs)
	slices.SortStableFunc(sorted, func(a, b Binding) int {
		if d := slices.Index(Kinds, a.Manifest.Capabilities[0].Kind) - slices.Index(Kinds, b.Manifest.Capabilities[0].Kind); d != 0 {
			return d
		}
		return strings.Compare(a.Manifest.ID, b.Manifest.ID)
	})
	return &Catalogue{bindings: sorted}, nil
}

// All is every connector, grouped by its first capability in the order of Kinds, then by id.
func (c *Catalogue) All() []Binding { return slices.Clone(c.bindings) }

// Get returns the connector id.
func (c *Catalogue) Get(id string) (Binding, bool) {
	for _, b := range c.bindings {
		if b.Manifest.ID == id {
			return b, true
		}
	}
	return Binding{}, false
}

// Implementing is every connector that implements kind, in the catalogue's order.
func (c *Catalogue) Implementing(kind Kind) []Binding {
	var out []Binding
	for _, b := range c.bindings {
		if _, ok := b.Manifest.Capability(kind); ok {
			out = append(out, b)
		}
	}
	return out
}
