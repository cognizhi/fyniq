package wiring

import "strings"

// SandboxEnabled says whether the deployment lets a Super admin make sandbox organisations (FR-INT-18): FYNIQ_SANDBOX=on. Off by default, and never
// a setting an organisation or a Super admin can change from the app: it is the operator's, per deployment.
func SandboxEnabled(getenv func(string) string) bool {
	return strings.EqualFold(strings.TrimSpace(getenv("FYNIQ_SANDBOX")), "on")
}

// SandboxWebhookHosts are the only hosts a sandbox organisation's webhooks may reach: FYNIQ_SANDBOX_WEBHOOK_HOSTS, exact host names separated by commas.
// None by default, so a sandbox sends no webhook.
func SandboxWebhookHosts(getenv func(string) string) []string {
	var out []string
	for _, h := range strings.Split(getenv("FYNIQ_SANDBOX_WEBHOOK_HOSTS"), ",") {
		if h = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), ".")); h != "" {
			out = append(out, h)
		}
	}
	return out
}
