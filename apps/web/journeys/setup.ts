// First-run setup, once per installation (FR-ADM-06), before any journey: on a fresh stack the web app opens on the
// setup page until it is done. The setup code comes from `make setup-code`, as the deployment step gives it; the
// installation's own organisation stays the setup's, and each journey makes its own.
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { chromium, type FullConfig } from "@playwright/test";
import { signIn } from "./harness";

export default async function setup(config: FullConfig): Promise<void> {
  const base = String(config.projects[0]!.use.baseURL);
  const done = (await (await fetch(`${base}/api/v1/setup`)).json()) as { done: boolean };
  if (done.done) return;
  const browser = await chromium.launch({ channel: "chrome" });
  try {
    const page = await (await browser.newContext({ baseURL: base })).newPage();
    await signIn(page, "journey-admin", "Journey Admin");
    const code = execFileSync("make", ["--no-print-directory", "setup-code"], { cwd: resolve(import.meta.dirname, "../../.."), encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] })
      .trim()
      .split("\n")
      .pop();
    const r = await page.request.post("/api/v1/setup", {
      headers: { "Sec-Fetch-Site": "same-origin" },
      data: { setup_code: code, display_name: "Journey Admin", organisation: { name: "Journeys Pte Ltd", base_currency: "SGD" } },
    });
    if (!r.ok()) throw new Error(`first-run setup: ${r.status()} ${await r.text()}`);
  } finally {
    await browser.close();
  }
}
