import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { RecoverAccount } from "./screens/Recovery";
import { ConfirmItsYou } from "./stepup";
import { answer, problem } from "./test/api";
import { renderPage } from "./test/render";
import { authenticator } from "./test/webauthn";

// NFR-A11Y-05, accessible authentication (WCAG 2.2 SC 3.3.8): signing in and confirming it's you take a password
// manager, a paste or a passkey, and never a cognitive test; a refused submit's summary takes focus and names each
// field. Fyniq's own sign-in steps are the provider list (the provider's page asks for the password), the step-up's
// passkey or a new sign-in, and account recovery's codes.

// The screens of the way in, whose fields people type or paste secrets into.
const wayIn = ["screens/SignIn.tsx", "stepup.tsx", "screens/Recovery.tsx", "screens/Join.tsx", "screens/Install.tsx"];

// problems lists what in text keeps a password manager or a paste out, or asks a cognitive test.
export function problems(file: string, text: string): string[] {
  const out: string[] = [];
  if (/onPaste|onCopy|onCut/.test(text)) out.push(`${file}: handles paste, copy or cut, which a password manager needs left alone`);
  if (/captcha|puzzle|type the characters|which of these/i.test(text)) out.push(`${file}: asks a cognitive test`);
  for (const m of text.matchAll(/<input[^>]*type="password"[^>]*>/g)) {
    if (!/autoComplete="(current|new)-password"/.test(m[0])) out.push(`${file}: a password field a password manager cannot fill`);
  }
  return out;
}

// req: NFR-A11Y-05
// req: experience/accessibility-and-performance NFR-A11Y-05 Accessible authentication
describe("accessible authentication", () => {
  it("leaves paste and password managers alone and asks no cognitive test on the way in", () => {
    const found = wayIn.flatMap((f) => problems(f, readFileSync(resolve(__dirname, f), "utf8")));
    expect(found).toEqual([]);
    expect(problems("x.tsx", '<input onPaste={(e) => e.preventDefault()} />')).toHaveLength(1);
    expect(problems("x.tsx", '<input type="password" />')).toHaveLength(1);
    expect(problems("x.tsx", "Type the characters you see")).toHaveLength(1);
  });

  it("offers a passkey to confirm it's you, and a new sign-in without one", async () => {
    authenticator();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <ConfirmItsYou provider="local" onConfirmed={() => undefined} />
      </QueryClientProvider>,
    );
    expect(await screen.findByRole("button", { name: "Use a passkey" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Sign in again" }).getAttribute("href")).toContain("provider=local");
  });

  it("takes a pasted recovery code, and a refused submit's summary takes focus and names the field", async () => {
    answer({
      "POST /v1/session/recovery": problem(422, "API-INVALID-01", "The recovery code is not one of yours.", "Check it against your saved codes."),
    });
    renderPage(RecoverAccount);
    fireEvent.click(await screen.findByRole("button", { name: "Lost every authenticator?" }));
    const code = screen.getByLabelText("Recovery code");
    expect(code.getAttribute("autocomplete")).toBe("one-time-code");
    fireEvent.paste(code, { clipboardData: { getData: () => "k7mq2-wxp4r" } });
    fireEvent.change(code, { target: { value: "k7mq2-wxp4r" } });
    expect((code as HTMLInputElement).value).toBe("k7mq2-wxp4r");
    fireEvent.change(screen.getByLabelText("Confirmation from your Owner or Administrator"), { target: { value: "FGHIJ-KLMNO" } });
    fireEvent.click(screen.getByRole("button", { name: "Recover my account" }));
    const summary = await screen.findByRole("alert");
    await waitFor(() => expect(summary.contains(document.activeElement)).toBe(true));
    expect(summary.textContent).toContain("The recovery code is not one of yours.");
  });
});
