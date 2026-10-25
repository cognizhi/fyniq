package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	netmail "net/mail"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/maildns"
	"github.com/cognizhi/fyniq/internal/mailhooks"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ReadConnectorNames reads FYNIQ_CONNECTOR_SECRETS: which Secret Manager secret holds each installation connector secret,
// as JSON of <connector>.<secret> → secret name (rev 0.139; names, never values). Unset is none: each secret is then
// connector-<connector>-<secret>.
func ReadConnectorNames(env func(string) string) (map[string]string, error) {
	out := map[string]string{}
	raw := env("FYNIQ_CONNECTOR_SECRETS")
	if raw == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("FYNIQ_CONNECTOR_SECRETS is not JSON of <connector>.<secret> to a secret name: %w", err)
	}
	for k, v := range out {
		if !strings.Contains(k, ".") || ports.CheckSecretName(v) != nil {
			return nil, fmt.Errorf("FYNIQ_CONNECTOR_SECRETS: %q → %q is not <connector>.<secret> to a secret name", k, v)
		}
	}
	return out, nil
}

// Runtime is the connector runtime of a service: the catalogue, the keys through a cache, the egress guard.
func (k *Kit) Runtime(clock ports.Clock) *connector.Runtime {
	return &connector.Runtime{Catalogue: k.Connectors, Secrets: connector.NewSecretCache(k.ConnectorSecrets, clock), Egress: k.Egress,
		SenderDomain: domainOf(k.Mail.From), Clock: clock}
}

func domainOf(from string) string {
	a, err := netmail.ParseAddress(from)
	if err != nil {
		return ""
	}
	return strings.ToLower(a.Address[strings.LastIndex(a.Address, "@")+1:])
}

// EmailProviders are the providers of each message's work (FR-INT-20, FR-INT-21): the installation's email connectors,
// read when the work starts, once the first-start bootstrap has installed them; until then, the deployment's providers,
// so a bootstrap that has not happened, or failed, never stops email (rev 0.139).
func (k *Kit) EmailProviders(rt *connector.Runtime) email.Providers {
	fallback := email.Static(k.Mail.Providers)
	return func(ctx context.Context, org uuid.UUID) (email.ProviderSet, error) {
		var mail connector.Mail
		installed := false
		if err := k.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Connectors().Installation(ctx)
			if err != nil {
				return err
			}
			installed = slices.ContainsFunc(all, func(c storage.InstalledConnector) bool {
				b, ok := k.Connectors.Get(c.Connector)
				return ok && b.OutboundEmail != nil
			})
			if !installed {
				return nil
			}
			secrets, err := u.Connectors().InstallationSecrets(ctx)
			if err != nil {
				return err
			}
			mail, err = rt.Mail(ctx, all, secrets, connector.OutboxEmail)
			return err
		}); err != nil {
			return email.ProviderSet{}, err
		}
		if !installed {
			return fallback(ctx, org)
		}
		return email.ProviderSet{Enabled: mail.Enabled, ByName: mail.Reconcile}, nil
	}
}

// BootstrapConnectors installs the email connectors from the deployment's settings on first start (the owner's
// decision of 25 Oct 2026, PRD-001 rev 0.139): the providers FYNIQ_EMAIL_PROVIDERS names, enabled, in that order, with
// their endpoints and the domains' DNS records, and each key as a reference to the newest enabled version of the secret
// FYNIQ_CONNECTOR_SECRETS names for it, never its value. It runs once, into an empty catalogue, as the service, in the
// installation's trail; before first-run setup there is no trail yet, and the next run does it. It returns how many it
// installed: 0 when it had run, when no provider is named, or before setup.
func BootstrapConnectors(ctx context.Context, k *Kit, env func(string) string, clock ports.Clock) (int, error) {
	if k.Connectors == nil || k.ConnectorSecrets == nil {
		return 0, nil
	}
	org, err := k.Store.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	done := false
	if err := k.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Connectors().Installation(ctx)
		done = len(all) > 0
		return err
	}); err != nil || done {
		return 0, err // installed before: the tick asks every 5 minutes, and nothing reaches Secret Manager
	}
	env = mailEnv(env)
	names := env("FYNIQ_EMAIL_PROVIDERS")
	if names == "" && env("FYNIQ_SMTP_ADDR") != "" {
		names = "smtp"
	}
	if names == "" {
		return 0, nil
	}
	dns := map[string]any{}
	if raw := env("FYNIQ_MAIL_DNS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &dns); err != nil {
			return 0, fmt.Errorf("FYNIQ_MAIL_DNS: %w", err)
		}
	}
	var rows []storage.BootstrapConnector
	for i, name := range strings.Split(names, ",") {
		name = strings.TrimSpace(name)
		b, ok := k.Connectors.Get(name)
		if !ok || b.OutboundEmail == nil || b.Manifest.Scope != connector.ScopeInstallation {
			return 0, fmt.Errorf("bootstrap: FYNIQ_EMAIL_PROVIDERS names %q, which is not an email connector of this release", name)
		}
		m := b.Manifest
		config := map[string]any{}
		set := func(key, value string) {
			if value != "" {
				config[key] = value
			}
		}
		switch name {
		case "smtp":
			set("address", env("FYNIQ_SMTP_ADDR"))
			set("domain", env("FYNIQ_MAIL_DOMAIN"))
		case "zeptomail":
			set("url", env("FYNIQ_ZEPTOMAIL_URL"))
		case "resend":
			set("url", env("FYNIQ_RESEND_URL"))
			set("api_base", env("FYNIQ_RESEND_API"))
		}
		if i == 0 && len(dns) > 0 { // the domains' records, as the deployment listed them, on the primary
			config["dns_records"] = dns
		}
		if err := m.Validate(config); err != nil {
			return 0, fmt.Errorf("bootstrap: %s: %w", name, err)
		}
		secrets := map[string]ports.SecretRef{}
		for _, s := range m.Secrets {
			secret := connector.Installations{Names: k.ConnectorNames}.SecretName(name, s.Name)
			ref, err := k.ConnectorSecrets.Latest(ctx, secret)
			switch {
			case err == nil:
				secrets[s.Name] = ref
			case errors.Is(err, ports.ErrNotFound) && !slices.Contains(s.UsedBy, "worker"):
				// a webhook secret not set yet: the connector sends, and its webhooks wait for it
			default:
				return 0, fmt.Errorf("bootstrap: %s's %s (secret %s) cannot be read: %w", name, s.Name, secret, err)
			}
		}
		rows = append(rows, storage.BootstrapConnector{NewConnector: storage.NewConnector{Connector: name, ManifestVersion: m.Version,
			ManifestHash: m.Hash(), SecretNames: secretNames(m), Consent: m.Consent(), Configuration: config, Position: i + 1}, Secrets: secrets})
	}
	n := 0
	err = k.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		n, err = u.Connectors().BootstrapInstallation(ctx, rows, clock.Now())
		return err
	})
	if code := storage.CodeOf(err); code == storage.CodeBootstrap || code == storage.CodeDuplicate {
		return 0, nil // installed before, by this or another instance
	}
	return n, err
}

func secretNames(m connector.Manifest) []string {
	out := make([]string, 0, len(m.Secrets))
	for _, s := range m.Secrets {
		out = append(out, s.Name)
	}
	return out
}

// CleanupJob runs the connectors' clean-up (FR-INT-26): replaced keys past their overlap, an uninstalled connector's
// keys, a refused change's key, and the installation changes nobody decided in time.
func (k *Kit) CleanupJob(clock ports.Clock) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if k.ConnectorSecrets == nil {
			return nil
		}
		_, err := connector.Cleanup{Store: k.Store, Installation: k.Store, Secrets: k.ConnectorSecrets, Clock: clock}.Run(ctx)
		return err
	}
}

// EmailDrift says how the deployment's FYNIQ_EMAIL_PROVIDERS differs from the email connectors installed, once the
// bootstrap has installed them: those variables are no longer read (rev 0.139), so a difference is worth a warning, not
// an error. Empty when they agree, when none is named, or before the bootstrap.
func EmailDrift(ctx context.Context, k *Kit, env func(string) string) (string, error) {
	named := strings.TrimSpace(env("FYNIQ_EMAIL_PROVIDERS"))
	if named == "" || k.Connectors == nil {
		return "", nil
	}
	org, err := k.Store.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var live []string
	if err := k.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Connectors().Installation(ctx)
		slices.SortStableFunc(all, func(x, y storage.InstalledConnector) int { return x.Position - y.Position })
		for _, c := range all {
			if b, ok := k.Connectors.Get(c.Connector); ok && b.OutboundEmail != nil && c.State == storage.ConnectorEnabled {
				live = append(live, c.Connector)
			}
		}
		return err
	}); err != nil || live == nil {
		return "", err
	}
	var want []string
	for _, n := range strings.Split(named, ",") {
		want = append(want, strings.TrimSpace(n))
	}
	if slices.Equal(want, live) {
		return "", nil
	}
	return fmt.Sprintf("FYNIQ_EMAIL_PROVIDERS says %s, but the enabled email connectors are %s: the connectors decide, and the variable "+
		"is no longer read; remove it or change the connectors in Settings › Integrations", strings.Join(want, ","), strings.Join(live, ",")), nil
}

// installation reads the installation's connectors and their secret versions in the installation organisation's unit of
// work; ok is false before first-run setup or before any connector is installed, when the deployment's settings apply.
func (k *Kit) installation(ctx context.Context) (all []storage.InstalledConnector, secrets []storage.ConnectorSecret, ok bool, err error) {
	if k.Connectors == nil {
		return nil, nil, false, nil
	}
	org, err := k.Store.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	err = k.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if all, err = u.Connectors().Installation(ctx); err != nil {
			return err
		}
		secrets, err = u.Connectors().InstallationSecrets(ctx)
		return err
	})
	return all, secrets, err == nil && len(all) > 0, err
}

// WebhookKeys are a provider's webhook keys for each request (mailhooks.Receiver.Keys, rev 0.139): its connector's
// current key and one still in its rotation overlap, newest first; mailhooks.ErrOff when the connector is not enabled
// or paused, or its key was never set. Before the bootstrap, the deployment's.
func (k *Kit) WebhookKeys(cache *connector.SecretCache, clock ports.Clock) func(ctx context.Context, provider string) ([]string, error) {
	static := map[string]string{"resend": k.Mail.ResendWebhookSecret, "zeptomail": k.Mail.ZeptoMailWebhookKey}
	return func(ctx context.Context, provider string) ([]string, error) {
		all, secrets, ok, err := k.installation(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			if static[provider] == "" {
				return nil, mailhooks.ErrOff
			}
			return []string{static[provider]}, nil
		}
		i := slices.IndexFunc(all, func(c storage.InstalledConnector) bool { return c.Connector == provider && c.State != storage.ConnectorUninstalled })
		b, known := k.Connectors.Get(provider)
		if i < 0 || !known || (all[i].State != storage.ConnectorEnabled && all[i].State != storage.ConnectorPaused) {
			return nil, mailhooks.ErrOff
		}
		var name string
		for _, w := range b.Manifest.Webhooks {
			name = w.Secret
		}
		now := clock.Now()
		var current, overlap []ports.SecretRef
		for _, s := range secrets {
			switch {
			case s.ConnectorID != all[i].ID || s.Name != name || s.DestroyedAt != nil:
			case s.ValidUntil == nil:
				current = append(current, s.Ref)
			case s.ValidUntil.After(now):
				overlap = append(overlap, s.Ref)
			}
		}
		var keys []string
		for _, ref := range append(current, overlap...) {
			v, err := cache.Get(ctx, ref)
			if err != nil {
				return nil, err
			}
			keys = append(keys, v)
		}
		if len(keys) == 0 {
			return nil, mailhooks.ErrOff
		}
		return keys, nil
	}
}

// InboundMail is the provider of each fetch of received mail (email.Fetcher.MailFrom): the enabled connector that
// receives email, or the deployment's before the bootstrap; ports.ErrNotFound when none is.
func (k *Kit) InboundMail(rt *connector.Runtime) func(ctx context.Context, org uuid.UUID) (ports.InboundMail, error) {
	return func(ctx context.Context, org uuid.UUID) (ports.InboundMail, error) {
		var in ports.InboundMail
		installed := false
		err := k.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Connectors().Installation(ctx)
			if err != nil {
				return err
			}
			installed = slices.ContainsFunc(all, func(c storage.InstalledConnector) bool {
				b, ok := k.Connectors.Get(c.Connector)
				return ok && b.InboundEmail != nil
			})
			if installed {
				in, _, err = rt.InboundEmail(ctx, u, connector.Authority{Trigger: connector.TriggerSystem, Rule: "a message received for an email-in address"})
			}
			return err
		})
		if !installed && err == nil {
			if k.Mail.Inbound == nil {
				return nil, ports.ErrNotFound
			}
			return k.Mail.Inbound, nil
		}
		return in, err
	}
}

// MailDNS are the records each sending domain needs, for the domain checks: the email connectors' (each provider's in
// its configuration, rev 0.139), or the deployment's before the bootstrap.
func (k *Kit) MailDNS(ctx context.Context) map[string][]maildns.Record {
	all, _, ok, err := k.installation(ctx)
	if err != nil || !ok {
		return k.Mail.DNS
	}
	out := map[string][]maildns.Record{}
	for _, c := range all {
		if c.State == storage.ConnectorUninstalled {
			continue
		}
		raw, err := json.Marshal(c.Configuration["dns_records"])
		if err != nil {
			continue
		}
		var recs map[string][]maildns.Record
		if json.Unmarshal(raw, &recs) != nil {
			continue
		}
		for d, rs := range recs {
			for _, r := range rs {
				if !slices.Contains(out[d], r) {
					out[d] = append(out[d], r)
				}
			}
		}
	}
	return out
}

// MailProviders are the enabled email connectors' names in order, for the email settings; the deployment's before the
// bootstrap.
func (k *Kit) MailProviders(ctx context.Context) []string {
	all, _, ok, err := k.installation(ctx)
	if err != nil || !ok {
		names := make([]string, 0, len(k.Mail.Providers))
		for _, p := range k.Mail.Providers {
			names = append(names, p.Name())
		}
		return names
	}
	slices.SortStableFunc(all, func(x, y storage.InstalledConnector) int { return x.Position - y.Position })
	var out []string
	for _, c := range all {
		if b, ok := k.Connectors.Get(c.Connector); ok && b.OutboundEmail != nil && c.State == storage.ConnectorEnabled {
			out = append(out, c.Connector)
		}
	}
	return out
}
