import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, problem } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { ConfirmRecovery, CoolingOff, MyRecoveryCodes, RecoverAccount } from "./Recovery";

const ten = Array.from({ length: 10 }, (_, i) => `ABCDE-FGH${i}J`.slice(0, 11));

// req: SEC-35
// req: G4-060
describe("Account recovery", () => {
  it("asks for recovery codes until the person has them, then shows ten once", async () => {
    const seen = answer({
      "GET /v1/session/recovery-codes": { status: 200, body: { unused: 0 } },
      "POST /v1/session/recovery-codes": { status: 200, body: { recovery_codes: ten } },
    });
    renderPage(MyRecoveryCodes);
    expect(await screen.findByText("Make your recovery codes")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Make recovery codes" }));
    const codes = await screen.findByRole("list", { name: "Recovery codes" });
    expect(within(codes).getAllByRole("listitem")).toHaveLength(10);
    expect(seen.some((s) => s.method === "POST" && s.path === "/v1/session/recovery-codes")).toBe(true);
    await noViolations(document.body);
  });

  it("recovers an account with a code and a confirmation, then shows the new codes and the cooling-off", async () => {
    const seen = answer({
      "POST /v1/session/recovery": { status: 200, body: { cooling_off_until: "2026-11-08T01:00:00Z", recovery_codes: ten } },
    });
    renderPage(RecoverAccount);
    fireEvent.click(await screen.findByRole("button", { name: "Lost every authenticator?" }));
    fireEvent.change(screen.getByLabelText("Recovery code"), { target: { value: " k7mq2-wxp4r " } });
    fireEvent.change(screen.getByLabelText("Confirmation from your Owner or Administrator"), { target: { value: "FGHIJ-KLMNO-PQRST-UVWXY-ZABCD-E" } });
    fireEvent.click(screen.getByRole("button", { name: "Recover my account" }));
    expect(await screen.findByText(/Your account is back/)).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ recovery_code: "k7mq2-wxp4r", confirmation_code: "FGHIJ-KLMNO-PQRST-UVWXY-ZABCD-E" });
    expect(screen.getByRole("list", { name: "Recovery codes" })).toBeTruthy();
  });

  it("says plainly when the codes do not recover the account", async () => {
    answer({ "POST /v1/session/recovery": problem(409, "STORAGE-RECOVERY-01", "The recovery is not valid, used or expired.") });
    renderPage(RecoverAccount);
    fireEvent.click(await screen.findByRole("button", { name: "Lost every authenticator?" }));
    fireEvent.change(screen.getByLabelText("Recovery code"), { target: { value: "AAAAA-AAAAA" } });
    fireEvent.change(screen.getByLabelText("Confirmation from your Owner or Administrator"), { target: { value: "X" } });
    fireEvent.click(screen.getByRole("button", { name: "Recover my account" }));
    expect(await screen.findByText(/not valid, used or expired/)).toBeTruthy();
  });

  it("gives an Owner a one-time confirmation for a member, shown once", async () => {
    answer({ "POST /v1/members/m-1/recovery-confirmations": { status: 201, body: { code: "FGHIJ-KLMNO-PQRST-UVWXY-ZABCD-E", expires_at: "2026-11-08T01:00:00Z" } } });
    renderPage(() => <ConfirmRecovery memberId="m-1" name="Wei Jie Lim" onClose={() => undefined} />);
    const dialog = await screen.findByRole("dialog", { name: "Confirm Wei Jie Lim's recovery?" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm the recovery" }));
    await waitFor(() => expect(within(dialog).getByLabelText("Confirmation code").textContent).toBe("FGHIJ-KLMNO-PQRST-UVWXY-ZABCD-E"));
  });

  it("shows the cooling-off, and nothing when there is none", () => {
    const { container, rerender } = render(<CoolingOff session={{ cooling_off_until: "2026-11-08T01:00:00Z" }} />);
    expect(container.textContent).toMatch(/Your account was recovered\. Until .*approving/);
    rerender(<CoolingOff session={{}} />);
    expect(container.textContent).toBe("");
  });
});
