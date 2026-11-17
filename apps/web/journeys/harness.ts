// The journeys' harness (p1-launch-readiness design D6): an organisation of the journey's own, with the sample company
// loaded through the engine, and its people, each signed in through the local sign-in page in a browser context of
// their own. No fixture answers the API: what a journey sees is what the stack does.
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { expect, type Browser, type BrowserContext, type Page } from "@playwright/test";

const repo = resolve(import.meta.dirname, "../../..");

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

// signIn signs page's context in through the local sign-in page, as a person does: the sign-in flow sends the browser to
// the page, the page back to the callback, and the callback sets the session cookie.
export async function signIn(page: Page, subject: string, name: string): Promise<void> {
  await page.goto("/api/auth/signin?provider=local&return_to=/");
  await page.fill("#subject", subject);
  await page.fill("#name", name);
  await page.click('button[value="sign-in"]');
  await page.waitForURL((u) => !u.pathname.startsWith("/api/auth"));
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
    const context = await browser.newContext();
    const page = await context.newPage();
    await signIn(page, `journey-${p.key}-${tag}`, p.name);
    await page.getByLabel("Invitation code").fill(made.invitations[i]!.code);
    await page.getByLabel("Your name").fill(p.name);
    await page.getByRole("button", { name: "Accept the invitation" }).click();
    await expect(page.getByRole("heading", { name: "Joining by invitation" })).toBeHidden();
    const session = await api<{ user_id?: string; organisation_id?: string }>(page, "/v1/session");
    if (session.organisation_id !== made.organisation || !session.user_id) {
      throw new Error(`${p.name} accepted the invitation but is not in the organisation: ${JSON.stringify(session)}`);
    }
    out[p.key] = { context, page, userId: session.user_id };
  }
  return { organisation: made.organisation, people: out };
}

// api reads the API as the person whose page it is, with their session, as the web app does.
export async function api<T>(page: Page, path: string): Promise<T> {
  const r = await page.request.get(`/api${path}`);
  if (!r.ok()) throw new Error(`GET /api${path}: ${r.status()} ${await r.text()}`);
  return (await r.json()) as T;
}
