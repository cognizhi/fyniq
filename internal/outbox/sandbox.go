package outbox

import "slices"

// SandboxNote is what a row's record says when a sandbox organisation's work was not run (FR-INT-18).
const SandboxNote = "a sandbox organisation sends nothing outside Fyniq: this work was not run"

// sandboxKinds are the only outbox kinds that run for a sandbox organisation (FR-INT-18), by default-deny: each one stays inside Fyniq or goes
// through a path that refuses or stubs the sandbox itself. email.send reaches only the sandbox sink (internal/email.Sandboxed); notify.fanout and
// notify.relay only queue email and webhook deliveries, each gated in its own right; the others read or write Fyniq's own records. A kind that
// is not listed, such as a bank sync, a payment link, a provider's events, the AI or an inbound fetch, is ended without running, so a new outbound
// kind is blocked for a sandbox until someone lists it here and a test says why.
var sandboxKinds = []string{
	"email.send", "notify.fanout", "notify.relay", "checks.refresh", "checks.request", "evidence.commit", "evidence.extend", "migration.validate",
	"reports.export", "security.sign_in", "bank.ingest", "oauth.client.remove", "oauth.agent_client.revoke", "oauth.agent.revoke", "oauth.subject.revoke",
	"tenant_export", "proposal.execute", "ai_proposal.execute",
}

// RunsInSandbox says whether outbox kind k runs for a sandbox organisation.
func RunsInSandbox(k string) bool { return slices.Contains(sandboxKinds, k) }
