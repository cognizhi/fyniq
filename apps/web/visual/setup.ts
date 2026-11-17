// The visual snapshots' one sign-in (p1-launch-readiness design D5): before any picture, the person scripts/visual.sh
// invited joins the organisation through the local sign-in page, and adds what the sample company lacks for a key page,
// a customer and a draft invoice dated within the business date's month. Their signed-in state and the invoice's page
// are kept for the run, so a failed picture never signs in again with a used invitation.
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { chromium, expect, type BrowserContext, type FullConfig } from "@playwright/test";
import { api, contact, signIn } from "../journeys/harness";

export const statePath = resolve(import.meta.dirname, "../test-results/visual-setup.json");

export interface Setup {
  storageState: Awaited<ReturnType<BrowserContext["storageState"]>>;
  invoice: string; // the draft invoice's page
}

export default async function setup(config: FullConfig): Promise<void> {
  const base = String(config.projects[0]!.use.baseURL);
  const made = JSON.parse(readFileSync(process.env.VISUAL_ORGANISATION ?? resolve(import.meta.dirname, "../../../.data/visual/organisation.json"), "utf8")) as {
    invitations: { name: string; code: string }[];
  };
  const browser = await chromium.launch();
  try {
    const context = await browser.newContext({ baseURL: base });
    const page = await context.newPage();
    await signIn(page, `visual-${Date.now().toString(36)}`, "Ann Owner");
    await page.getByLabel("Invitation code").fill(made.invitations[0]!.code);
    await page.getByLabel("Your name").fill("Ann Owner");
    await page.getByRole("button", { name: "Accept the invitation" }).click();
    await expect(page.getByRole("heading", { name: "Joining by invitation" })).toBeHidden();

    const customer = await contact(page, "Acme Analytics Pte Ltd", { customer: true });
    const accounts = await api<{ items: { id: string; code: string }[] }>(page, "/v1/accounts?page_size=500");
    const revenue = accounts.items.find((a) => a.code === "4010");
    if (!revenue) throw new Error("the sample company has no 4010 Subscription revenue");
    const r = await page.request.post("/api/v1/invoices", {
      headers: { "Sec-Fetch-Site": "same-origin" },
      data: {
        customer_id: customer, reference: "PO-1042", date: "2026-12-15", currency: "SGD",
        lines: [{ account_id: revenue.id, description: "Analytics platform, December", gst_code: "SR", quantity: "1", unit_price: "3000.00" }],
      },
    });
    if (r.status() !== 201) throw new Error(`the draft invoice: ${r.status()} ${await r.text()}`);
    const invoice = `/sales/invoices/${((await r.json()) as { id: string }).id}`;

    mkdirSync(resolve(statePath, ".."), { recursive: true });
    const out: Setup = { storageState: await context.storageState(), invoice };
    writeFileSync(statePath, JSON.stringify(out));
  } finally {
    await browser.close();
  }
}
