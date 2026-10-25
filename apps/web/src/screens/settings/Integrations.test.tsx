import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import type { Mode } from "../../registry";
import { IntegrationDetail } from "./IntegrationDetail";
import { Integrations } from "./Integrations";

const none = { data_classes: [], destinations: [], scopes: [], webhooks: [] };

const zeptoEntry = {
  id: "zeptomail",
  name: "Zoho ZeptoMail",
  publisher: "Fyniq",
  version: "1.0.1",
  summary: "Sends the platform's email through Zoho ZeptoMail.",
  scope: "installation",
  capabilities: [{ kind: "outbound-email", trigger: "system-rule", trigger_rule: "A message the outbox holds for sending.", on_disable: "hold", in_flight: "reconcile" }],
  configuration: {
    type: "object",
    properties: {
      url: { type: "string", title: "Email endpoint", description: "The account's email endpoint.", default: "https://cpaas.zoho.com/v1.1/email" },
      dns_records: { type: "object", title: "DNS records", description: "The records each sending domain needs." },
    },
  },
  data_classes: ["personal-data", "business-figures"],
  regions: ["US"],
  destinations: [{ host: "cpaas.zoho.com", port: 443 }],
  secrets: [
    { name: "send_token", title: "Send token", description: "The Mail Agent's send token.", rotation_overlap_hours: 1 },
    { name: "webhook_key", title: "Webhook authentication key", description: "Signs ZeptoMail's events.", rotation_overlap_hours: 24 },
  ],
  webhooks: [{ path: "/api/webhooks/email/zeptomail", events: ["hardbounce"], registered_by: "operator" }],
  health_check: "Counts ZeptoMail's refusals in recent sends.",
  test_action: "Sends one test message to the organisation's own email address.",
  local_substitute: "Mailpit in the local stack.",
};
const smtpEntry = {
  ...zeptoEntry,
  id: "smtp",
  name: "SMTP",
  summary: "Sends email through a mail server you run.",
  regions: ["configured"],
  destinations: [{ configured: "address" }],
  secrets: [],
  webhooks: [],
  configuration: { type: "object", required: ["address"], properties: { address: { type: "string", title: "Server address", description: "host:port." } } },
};

const zepto = {
  id: "c-1",
  connector: "zeptomail",
  name: "Zoho ZeptoMail",
  installation: true,
  state: "enabled",
  health: "failing",
  position: 1,
  manifest_version: "1.0.0",
  release_version: "1.0.1",
  upgrade_widens: true,
  added: { ...none, data_classes: ["know-how"] },
  removed: none,
  configuration: { url: "https://cpaas.zoho.com/v1.1/email" },
  secrets: [
    { name: "send_token", set: true, replaced_at: "2026-10-20T02:00:00Z" },
    { name: "webhook_key", set: false },
  ],
  test: { at: "2026-10-24T01:00:00Z", passed: false, reason: "ZeptoMail refused the send token." },
  installed_at: "2026-10-20T01:00:00Z",
  installed_by: "user:u-1",
  changed_at: "2026-10-24T01:00:00Z",
  version: 4,
};

const change = {
  id: "ch-1",
  connector: "zeptomail",
  connector_id: "c-1",
  step: "resume",
  summary: "Resume Zoho ZeptoMail",
  status: "pending",
  envelope: '{"change":{"step":"resume"}}',
  envelope_hash: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
  requested_by: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d99",
  requested_at: "2026-10-24T02:00:00Z",
  expires_at: "2026-10-31T02:00:00Z",
};

const superAdmin = { ...ownerSession, super_admin: true };

function catalogue(extra = {}) {
  return answer({
    "GET /v1/connector-catalogue": { status: 200, body: { items: [zeptoEntry, smtpEntry] } },
    "GET /v1/organisation/connectors": { status: 200, body: { items: [] } },
    "GET /v1/installation/connectors": { status: 200, body: { items: [zepto] } },
    "GET /v1/installation/changes": { status: 200, body: { items: [change] } },
    ...extra,
  });
}

// req: FR-INT-25
// req: FR-INT-26
describe("Integrations", () => {
  it("shows the installed connectors with status, health, secrets and upgrade, the catalogue by kind with its manifest, and axe finds nothing", async () => {
    catalogue();
    renderPage(Integrations, { path: "/settings/integrations", session: superAdmin as never });
    const table = await screen.findByRole("table");
    const row = within(table).getByRole("link", { name: "Zoho ZeptoMail" }).closest("tr") as HTMLElement;
    expect(within(row).getByText("Enabled")).toBeTruthy();
    expect(within(row).getByText("Failing")).toBeTruthy();
    expect(within(row).getByText("1 not set")).toBeTruthy();
    expect(within(row).getByText(/1\.0\.1 asks for new access/)).toBeTruthy();
    expect(screen.getByText("Zoho ZeptoMail failed its last test")).toBeTruthy();
    const email = screen.getByRole("list", { name: "Outbound email" });
    expect(within(email).getAllByText("sends personal data and business figures")).toHaveLength(2);
    expect(within(email).getAllByText(/processed in United States/)).toHaveLength(1);
    expect(within(email).getByText("Installed")).toBeTruthy();
    expect(within(email).getByRole("button", { name: "Install: SMTP" })).toBeTruthy();
    await noViolations(document.body);
  });

  it("installs a catalogue connector for the installation, and says when it waits for the other Super admin", async () => {
    const seen = catalogue({ "POST /v1/installation/connectors": { status: 202, body: { ...change, id: "ch-2", step: "install", connector_id: undefined, summary: "Install SMTP" } } });
    renderPage(Integrations, { path: "/settings/integrations", session: superAdmin as never });
    fireEvent.click(await screen.findByRole("button", { name: "Install: SMTP" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("where its server is")).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Install" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/installation/connectors"));
    const post = seen.find((s) => s.method === "POST");
    expect(post?.body).toEqual({ connector: "smtp" });
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
    expect(await screen.findByText("Waiting for a second approver")).toBeTruthy();
  });

  it("lets a Super admin approve the other's change over the envelope's hash", async () => {
    const seen = catalogue({ "POST /v1/installation/changes/ch-1/approve": { status: 200, body: { ...change, status: "approved" } } });
    renderPage(Integrations, { path: "/settings/integrations", session: superAdmin as never });
    fireEvent.click(await screen.findByRole("button", { name: "Approve: Resume Zoho ZeptoMail" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/installation/changes/ch-1/approve"));
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ envelope_hash: change.envelope_hash });
  });

  it("shows an Owner only the organisation's connectors and no installation actions", async () => {
    answer({
      "GET /v1/connector-catalogue": { status: 200, body: { items: [zeptoEntry, smtpEntry] } },
      "GET /v1/organisation/connectors": { status: 200, body: { items: [] } },
    });
    renderPage(Integrations, { path: "/settings/integrations" });
    expect(await screen.findByText(/No connectors installed here yet/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Install:/ })).toBeNull();
    expect(calls()).not.toContain("GET /v1/installation/connectors");
  });
});

const emailSettings = {
  providers: [
    { name: "zeptomail", role: "primary" },
    { name: "resend", role: "failover" },
  ],
  senders: [{ purpose: "invoices", from: "invoices@mail.example.com", address: "invoices@mail.example.com", platform: false, etag: '"2"' }],
  domains: ["mail.example.com"],
  domain_checks: [
    {
      domain: "mail.example.com",
      ok: false,
      dmarc: "none",
      checked_at: "2026-10-24T01:00:00Z",
      records: [{ type: "TXT", name: "zmail._domainkey.mail.example.com", value: "v=DKIM1; p=KEY", status: "missing" }],
    },
  ],
};

function detail(extra = {}) {
  return answer({
    "GET /v1/installation/connectors/c-1": { status: 200, body: zepto, headers: { ETag: '"4"' } },
    "GET /v1/connector-catalogue": { status: 200, body: { items: [zeptoEntry, smtpEntry] } },
    "GET /v1/installation/changes": { status: 200, body: { items: [change] } },
    "GET /v1/organisation/email": { status: 200, body: emailSettings },
    "GET /v1/organisation/email/suppressions": {
      status: 200,
      body: { items: [{ id: "s-1", address: "ap@example.com", reason: "hard_bounce", suppressed_at: "2026-10-08T01:00:00Z" }] },
    },
    ...extra,
  });
}

function openDetail(mode: Mode = "expert") {
  return renderPage(IntegrationDetail, { path: "/settings/integrations/c-1?scope=installation", route: "/settings/integrations/$connector", session: superAdmin as never, mode });
}

// req: FR-INT-20
// req: FR-INT-26
// req: FR-INT-27
describe("A connector's page", () => {
  it("shows the lifecycle, the failed test's reason, health, the manifest and the waiting change, and axe finds nothing", async () => {
    detail();
    openDetail();
    expect(await screen.findByRole("heading", { level: 1, name: "Zoho ZeptoMail" })).toBeTruthy();
    expect(within(screen.getByRole("list", { name: "Lifecycle" })).getByText("Enable")).toBeTruthy();
    expect(screen.getAllByText("ZeptoMail refused the send token.").length).toBeGreaterThan(0);
    expect(await screen.findByText("Resume Zoho ZeptoMail is waiting for a second approver")).toBeTruthy();
    expect(screen.getByText("Primary for email")).toBeTruthy();
    expect(screen.getByText("cpaas.zoho.com")).toBeTruthy();
    await noViolations(document.body);
  });

  it("saves the configuration from its schema and sets a write-only secret over the ETag", async () => {
    const seen = detail({
      "PUT /v1/installation/connectors/c-1/configuration": { status: 202, body: { ...change, id: "ch-3", step: "configure", summary: "Configure Zoho ZeptoMail" } },
      "PUT /v1/installation/connectors/c-1/secrets/webhook_key": { status: 202, body: { ...change, id: "ch-4", step: "secret", summary: "Replace Zoho ZeptoMail's webhook_key" } },
    });
    openDetail();
    fireEvent.click(await screen.findByRole("tab", { name: "Configuration" }));
    fireEvent.change(screen.getByLabelText("DNS records"), { target: { value: '{"mail.example.com":[{"type":"TXT","name":"a","value":"b"}]}' } });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(calls()).toContain("PUT /v1/installation/connectors/c-1/configuration"));
    const put = seen.find((s) => s.path.endsWith("/configuration"));
    expect(put?.headers.get("If-Match")).toBe('"4"');
    expect(put?.body).toEqual({ configuration: { url: "https://cpaas.zoho.com/v1.1/email", dns_records: { "mail.example.com": [{ type: "TXT", name: "a", value: "b" }] } } });
    fireEvent.change(screen.getByLabelText(/Webhook authentication key/, { selector: "input" }), { target: { value: "whsec-example" } });
    fireEvent.click(screen.getByRole("button", { name: "Set: Webhook authentication key" }));
    await waitFor(() => expect(calls()).toContain("PUT /v1/installation/connectors/c-1/secrets/webhook_key"));
    expect(seen.find((s) => s.path.endsWith("/webhook_key"))?.body).toEqual({ value: "whsec-example" });
    expect(await screen.findByText("Waiting for a second approver")).toBeTruthy();
  });

  it("refuses a configuration that is not valid JSON, naming the field, and sends nothing", async () => {
    detail();
    openDetail();
    fireEvent.click(await screen.findByRole("tab", { name: "Configuration" }));
    fireEvent.change(screen.getByLabelText("DNS records"), { target: { value: "{not json" } });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByText(/DNS records is not valid JSON/)).toBeTruthy();
    expect(calls()).not.toContain("PUT /v1/installation/connectors/c-1/configuration");
  });

  it("shows the upgrade's diff and consents to it", async () => {
    const seen = detail({ "POST /v1/installation/connectors/c-1/steps": { status: 202, body: { ...change, id: "ch-5", step: "consent", summary: "Consent to Zoho ZeptoMail 1.0.1" } } });
    openDetail();
    fireEvent.click(await screen.findByRole("tab", { name: "Upgrade" }));
    expect(within(screen.getByRole("list", { name: "Manifest changes" })).getByText("New data class: know-how")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Consent to the upgrade" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/installation/connectors/c-1/steps"));
    expect(seen.find((s) => s.path.endsWith("/steps"))?.body).toEqual({ step: "consent" });
  });

  it("shows the senders, the domain's checks, the failover order and the suppression list, and releases an address", async () => {
    detail({ "POST /v1/organisation/email/suppressions/s-1/release": { status: 200, body: {} } });
    openDetail();
    fireEvent.click(await screen.findByRole("tab", { name: "Senders and domain" }));
    expect(await screen.findByText("invoices@mail.example.com")).toBeTruthy();
    expect(screen.getByText("Missing: add it at your DNS host")).toBeTruthy();
    expect(screen.getByText(/Only reports/)).toBeTruthy();
    const order = screen.getByRole("list", { name: "Failover order" });
    expect(within(order).getByText("Zoho ZeptoMail")).toBeTruthy();
    expect(within(order).getByText("Failover")).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: "Release: ap@example.com" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/organisation/email/suppressions/s-1/release"));
    await noViolations(document.body);
  });

  // req: QA-UI-04
  it("sends the same request for the same action in Basic and Expert mode", async () => {
    const sent: unknown[] = [];
    for (const mode of ["basic", "expert"] as const) {
      const seen = detail({ "POST /v1/installation/connectors/c-1/steps": { status: 200, body: { ...zepto, state: "paused" }, headers: { ETag: '"5"' } } });
      const { unmount } = openDetail(mode);
      fireEvent.click((await screen.findAllByRole("button", { name: "Pause" }))[0] as HTMLElement);
      await waitFor(() => expect(calls()).toContain("POST /v1/installation/connectors/c-1/steps"));
      const post = seen.find((s) => s.path.endsWith("/steps"));
      sent.push({ path: post?.path, body: post?.body, ifMatch: post?.headers.get("If-Match") });
      unmount();
    }
    expect(sent[0]).toEqual({ path: "/v1/installation/connectors/c-1/steps", body: { step: "pause" }, ifMatch: '"4"' });
    expect(sent[1]).toEqual(sent[0]);
  });
});
