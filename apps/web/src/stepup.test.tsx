import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { api } from "./api/client";
import { ConfirmItsYou, guarded, stepUp } from "./stepup";
import { answer, calls, problem } from "./test/api";
import { noViolations } from "./test/axe";
import { authenticator, credentialJSON, noAuthenticator } from "./test/webauthn";

const challenge = { status: 200, body: { challenge_id: "c-1", expires_at: "2026-10-20T06:05:00Z", options: { publicKey: { challenge: "AQID", rpId: "localhost" } } } };
const steppedUp = { status: 200, body: { passkey_id: "k-1", step_up_method: "passkey", stepped_up_until: "2026-10-20T06:10:00Z" } };

function renderConfirm(onConfirmed = () => {}) {
  const queries = new QueryClient();
  return render(
    <QueryClientProvider client={queries}>
      <ConfirmItsYou provider="local" onConfirmed={onConfirmed} />
    </QueryClientProvider>,
  );
}

// req: FR-ADM-02
// req: SEC-01
describe("confirming it's you", () => {
  it("answers the API's challenge with a passkey", async () => {
    const credentials = authenticator();
    const seen = answer({ "POST /v1/session/step-up/challenge": challenge, "POST /v1/session/step-up": steppedUp });
    expect(await stepUp()).toEqual({ ok: true });
    expect(credentials.get).toHaveBeenCalledOnce();
    expect(seen[1]?.body).toEqual({ challenge_id: "c-1", credential: credentialJSON });
  });

  it("says when there is no passkey, and offers to sign in again", async () => {
    authenticator();
    answer({ "POST /v1/session/step-up/challenge": problem(409, "API-PASSKEY-02", "You have no passkey.") });
    renderConfirm();
    fireEvent.click(screen.getByRole("button", { name: "Use a passkey" }));
    expect(await screen.findByText(/You have no passkey yet/)).toBeTruthy();
    const again = screen.getByRole("link", { name: "Sign in again" });
    expect(again.getAttribute("href")).toBe("/api/auth/signin?provider=local&return_to=%2F");
    await noViolations(document.body);
  });

  it("explains a refused answer and takes focus", async () => {
    authenticator();
    answer({ "POST /v1/session/step-up/challenge": challenge, "POST /v1/session/step-up": problem(403, "API-PASSKEY-01", "The passkey's answer was refused.") });
    renderConfirm();
    fireEvent.click(screen.getByRole("button", { name: "Use a passkey" }));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("API-PASSKEY-01");
    // The refusal takes focus in an effect after it appears: wait for it, as a loaded CI runner may assert first.
    await waitFor(() => expect(document.activeElement).toBe(alert));
  });

  it("treats a cancelled passkey as a choice, not an error", async () => {
    authenticator({ refuse: "NotAllowedError" });
    answer({ "POST /v1/session/step-up/challenge": challenge });
    renderConfirm();
    fireEvent.click(screen.getByRole("button", { name: "Use a passkey" }));
    expect(await screen.findByText(/cancelled, or took too long/)).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("offers only signing in again in a browser without passkeys", () => {
    noAuthenticator();
    renderConfirm();
    expect(screen.queryByRole("button", { name: "Use a passkey" })).toBeNull();
    expect(screen.getByText(/cannot use passkeys/)).toBeTruthy();
  });

  it("runs an action once more after the person confirms, and not when they cancel", async () => {
    let n = 0;
    answer({ "POST /v1/session/devices/sign-out-others": () => (++n === 1 ? problem(403, "API-AUTHZ-05") : { status: 200, body: { ended: 2 } }) });
    const out = await guarded(
      () => Promise.resolve(true),
      () => api.POST("/v1/session/devices/sign-out-others"),
    );
    expect(out.data).toEqual({ ended: 2 });
    n = 0;
    const refused = await guarded(
      () => Promise.resolve(false),
      () => api.POST("/v1/session/devices/sign-out-others"),
    );
    expect(refused.error).toBeTruthy();
    await waitFor(() => expect(calls()).toHaveLength(3));
  });
});
