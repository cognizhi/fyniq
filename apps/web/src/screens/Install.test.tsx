import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer } from "../test/api";
import { noViolations } from "../test/axe";
import { invalid } from "../test/invalid";
import { Install } from "./Install";

const code = "ABCDE-FGHIJ-KLMNO-PQRST";
const open = { "GET /v1/setup": { status: 200, body: { done: false } }, "GET /v1/session": { status: 200, body: { provider: "Google" } } };

// req: FR-ADM-06
describe("first-run setup", () => {
  // QA-UI-01: each step marks the field it cannot read, described by the message the summary shows.
  it("marks each field it cannot read, step by step, and takes the person back to a setup code the API refuses", async () => {
    const seen = answer({ ...open, "POST /v1/setup": { status: 422, body: { code: "API-SETUP-01", detail: "The setup code is not valid." } } });
    render(<Install />);
    fireEvent.click(await screen.findByRole("button", { name: "Continue" }));
    await invalid(document.body, /^Your name/, "Enter your name, as other people will see it.");
    expect(document.activeElement).toBe(screen.getByRole("alert"));
    fireEvent.change(screen.getByLabelText(/^Your name/), { target: { value: "Wei Ling Tan" } });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    fireEvent.change(await screen.findByLabelText(/^One-time setup code/), { target: { value: "ABCDE" } });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await invalid(document.body, /^One-time setup code/, "Enter the whole setup code, as it was printed.");
    fireEvent.change(screen.getByLabelText(/^One-time setup code/), { target: { value: code } });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    fireEvent.click(await screen.findByRole("button", { name: "Create organisation" }));
    await invalid(document.body, /^Company name/, "Enter the company's name.");
    expect(seen.filter((s) => s.method === "POST")).toEqual([]);
    fireEvent.change(screen.getByLabelText(/^Company name/), { target: { value: "Cognizhi Pte. Ltd." } });
    fireEvent.click(screen.getByRole("button", { name: "Create organisation" }));
    await invalid(
      document.body,
      /^One-time setup code/,
      "That setup code isn't right, or this installation is set up already. Copy it again from the make setup-code output or Secret Manager.",
    );
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ setup_code: code, display_name: "Wei Ling Tan", organisation: { name: "Cognizhi Pte. Ltd.", base_currency: "SGD" } });
    await noViolations(document.body);
  });

  it("says the page is closed once the installation is set up", async () => {
    answer({ ...open, "GET /v1/setup": { status: 200, body: { done: true } } });
    render(<Install />);
    await waitFor(() => expect(screen.getByRole("heading", { name: "This installation is set up" })).toBeTruthy());
  });
});
