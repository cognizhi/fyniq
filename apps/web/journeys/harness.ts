// The journeys' harness (p1-launch-readiness design D6): an organisation of the journey's own, with the sample company
// loaded through the engine, and its people, each signed in through the local sign-in page in a browser context of
// their own. No fixture answers the API: what a journey sees is what the stack does.
import { execFileSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { test as base, expect, type Browser, type BrowserContext, type Page, type Response } from "@playwright/test";

const repo = resolve(import.meta.dirname, "../../..");

// The people a journey signed in: on a failure each one's page is attached as an ARIA snapshot, to read what they saw;
// then their browsers close.
const signed: { name: string; context: BrowserContext; page: Page }[] = [];

// test is Playwright's, with the people a journey signed in looked after when it ends. Journeys import it from here.
export const test = base.extend<{ people: void }>({
  people: [
    async ({}, use, info) => {
      await use();
      for (const s of signed.splice(0)) {
        if (info.status !== info.expectedStatus && !s.page.isClosed()) {
          const seen = await s.page.locator("body").ariaSnapshot().catch((e: unknown) => `no snapshot: ${String(e)}`);
          const path = info.outputPath(`${s.name.replaceAll(" ", "-")}.aria.txt`);
          writeFileSync(path, `${s.page.url()}\n${seen}\n`);
          await info.attach(`what ${s.name} saw`, { path, contentType: "text/plain" });
        }
        await s.context.close();
      }
    },
    { auto: true },
  ],
});
export { expect };

export interface Person {
  key: string; // how the journey names them
  name: string;
  roles: string[];
}

export interface Signed {
  context: BrowserContext;
  page: Page;
  userId: string;
}

export interface Journey {
  organisation: string;
  people: Record<string, Signed>;
}

// The API counts what a person does before they belong to an organisation per client address, a minute at a time
// (SEC-23): 30 sign-in requests and 10 joins a minute by default, and every journey comes from one address. So the
// journeys let at most four people sign in and join in any minute, and a request answered 429 is sent again after
// its Retry-After, as a person would try again (CI runs them just after the DAST scan has signed in too).
const started: number[] = [];
async function pace(page: Page): Promise<void> {
  for (;;) {
    const now = Date.now();
    while (started.length && now - started[0]! > 61_000) started.shift();
    if (started.length < 4) break;
    await page.waitForTimeout(61_000 - (now - started[0]!));
  }
  started.push(Date.now());
}

// limited says how long to wait for a response the per-address limits refused, or 0 for any other.
function limited(status: number | undefined, headers: Record<string, string> | undefined): number {
  return status === 429 ? Number(headers?.["retry-after"] ?? 60) * 1000 + 1000 : 0;
}

// signIn signs page's context in through the local sign-in page, as a person does: the sign-in flow sends the browser
// to the page, the page back to the callback, and the callback sets the session cookie.
export async function signIn(page: Page, subject: string, name: string): Promise<void> {
  for (let attempt = 1; ; attempt++) {
    let refused = 0;
    const watch = (r: Response) => {
      if (r.url().includes("/api/auth/")) refused ||= limited(r.status(), r.headers());
    };
    page.on("response", watch);
    try {
      const first = await page.goto("/api/auth/signin?provider=local&return_to=/");
      if (!refused) {
        await page.fill("#subject", subject);
        await page.fill("#name", name);
        await page.click('button[value="sign-in"]');
        const done = await page.waitForURL((u) => !u.pathname.startsWith("/api/auth"), { timeout: 15_000 }).then(
          () => true,
          () => false,
        );
        if (done && !refused) return;
      }
      if (!refused || attempt === 4) throw new Error(`${name} could not sign in (${first?.status()}) at ${page.url()}`);
      await page.waitForTimeout(refused);
    } finally {
      page.off("response", watch);
    }
  }
}

// organisation makes a journey's organisation: cmd/seed makes it with an invitation for each person and loads the
// sample company into it (DEV-04), checking its trial balance against the seed's own totals; then each person signs in,
// which leaves a new sign-in on the "Joining by invitation" page, and accepts theirs, as a person invited does
// (FR-ADM-01). Names and subjects carry a run's tag, so runs never share an organisation or a person.
export async function organisation(browser: Browser, name: string, people: Person[]): Promise<Journey> {
  const tag = Date.now().toString(36);
  const args = ["run", "-tags", "local", "./cmd/seed", "--new-organisation", `${name} (${tag})`];
  for (const p of people) args.push("--invite", `${p.name}:${p.roles.join(",")}`);
  const made = JSON.parse(execFileSync("go", args, { cwd: repo, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] })) as {
    organisation: string;
    invitations: { name: string; code: string }[];
  };
  const out: Journey["people"] = {};
  for (const [i, p] of people.entries()) {
    out[p.key] = await join(browser, `${p.key}-${tag}`, p.name, made.invitations[i]!.code);
    const session = await api<{ organisation_id?: string }>(out[p.key]!.page, "/v1/session");
    if (session.organisation_id !== made.organisation) throw new Error(`${p.name} joined another organisation: ${JSON.stringify(session)}`);
  }
  return { organisation: made.organisation, people: out };
}

// join signs a new person in through the local sign-in page, in a browser context of their own, and accepts their
// invitation on "Joining by invitation", where a sign-in with no user lands.
export async function join(browser: Browser, key: string, name: string, code: string): Promise<Signed> {
  const context = await browser.newContext();
  const page = await context.newPage();
  signed.push({ name, context, page });
  await pace(page);
  await signIn(page, `journey-${key}`, name);
  await page.getByLabel("Invitation code").fill(code);
  await page.getByLabel("Your name").fill(name);
  for (let attempt = 1; ; attempt++) {
    const [accepted] = await Promise.all([
      page.waitForResponse((r) => r.url().endsWith("/api/v1/members") && r.request().method() === "POST"),
      page.getByRole("button", { name: "Accept the invitation" }).click(),
    ]);
    const wait = limited(accepted.status(), accepted.headers());
    if (!wait || attempt === 4) break;
    await page.waitForTimeout(wait);
  }
  await expect(page.getByRole("heading", { name: "Joining by invitation" })).toBeHidden();
  const session = await api<{ user_id?: string }>(page, "/v1/session");
  if (!session.user_id) throw new Error(`${name} accepted the invitation but has no user: ${JSON.stringify(session)}`);
  return { context, page, userId: session.user_id };
}

// expert switches the person's experience mode to Expert, where the books' own pages are (§7.8).
export async function expert(page: Page): Promise<void> {
  const radio = page.getByRole("radiogroup", { name: "Experience mode" }).getByRole("radio", { name: "Expert" });
  if (!(await radio.isChecked())) await radio.click();
  await expect(radio).toBeChecked();
}

// contact adds a contact on the Contacts page, as a customer, a vendor or both, and returns its ID from the API.
export async function contact(page: Page, name: string, is: { customer?: boolean; vendor?: boolean }): Promise<string> {
  await page.goto("/contacts");
  await page.getByRole("button", { name: "New contact" }).click();
  const dialog = page.getByRole("dialog", { name: "New contact" });
  await dialog.getByLabel("Legal name").fill(name);
  if (is.customer) await dialog.getByRole("checkbox", { name: "A customer" }).check();
  if (is.vendor) await dialog.getByRole("checkbox", { name: "A vendor" }).check();
  await dialog.getByRole("button", { name: "Add the contact" }).click();
  await expect(dialog).toBeHidden();
  const all = await api<{ items: { id: string; legal_name: string }[] }>(page, `/v1/contacts?role=${is.customer ? "customer" : "vendor"}&page_size=200`);
  const c = all.items.find((x) => x.legal_name === name);
  if (!c) throw new Error(`the contact ${name} is not in the API's contacts`);
  return c.id;
}

// Money is the API's amount: a decimal string with its currency (ENG-12).
export interface Money {
  amount: string;
  currency: string;
}

// sgd is an amount in Singapore dollars, as the API writes it.
export const sgd = (amount: string): Money => ({ amount, currency: "SGD" });

// api reads the API as the person whose page it is, with their session, as the web app does.
export async function api<T>(page: Page, path: string): Promise<T> {
  const r = await page.request.get(`/api${path}`);
  if (!r.ok()) throw new Error(`GET /api${path}: ${r.status()} ${await r.text()}`);
  return (await r.json()) as T;
}
