import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { noViolations } from "../test/axe";
import { fieldName, Refusal, sentence } from "./Refusal";

// req: ENG-23
describe("a refusal", () => {
  it("says what is wrong and how to fix it in sentences, naming each field; axe finds nothing", async () => {
    render(
      <Refusal
        title="The export could not be asked for"
        problem={{
          code: "API-INPUT-01",
          detail: "The request has 2 problems",
          fix: "Correct the fields below",
          errors: [
            { field: "body.to", message: "is not a date", fix: "Send the day as YYYY-MM-DD" },
            { field: "body.lines[0].account_id", message: "the account is a group, which takes no lines.", fix: "Choose one of its accounts" },
          ],
        }}
      />,
    );
    const alert = screen.getByRole("alert", { name: "The export could not be asked for" });
    expect(alert.textContent).toBe(
      "The export could not be asked forThe request has 2 problems. Correct the fields below. (API-INPUT-01)" +
        "To: is not a date. Send the day as YYYY-MM-DD.Lines 1 account id: the account is a group, which takes no lines. Choose one of its accounts.",
    );
    expect(document.activeElement).toBe(alert);
    await noViolations(document.body);
  });

  it("ends a sentence once, and reads a field's path as words", () => {
    expect([sentence("Try again in a moment"), sentence("The service is busy."), sentence("Why?"), sentence(" ")]).toEqual([
      "Try again in a moment.",
      "The service is busy.",
      "Why?",
      "",
    ]);
    expect([fieldName("query.as_at_revision"), fieldName("from"), fieldName("body.parts[2].amount")]).toEqual(["As at revision", "From", "Parts 3 amount"]);
  });
});
