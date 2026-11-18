import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AnswerText } from "./AnswerText";

// req: AI-08
describe("an answer's text", () => {
  it("renders Fyniq's Markdown subset as text, each figure from its fact and what could not be established; axe finds nothing", async () => {
    renderPage(() => (
      <AnswerText
        segments={[
          { text: "Revenue in **October** was " },
          { fact: "f-1", figure: "12,400.00 SGD" },
          { text: ", up on *September*.\n- see `4010`\n- open [the statement](/reports/statements/pl)\n\nOutside: [a site](https://example.com)." },
          { missing: "the November budget." },
        ]}
      />
    ));
    expect(await screen.findByText("October")).toBeTruthy();
    expect(screen.getByText("October").tagName).toBe("STRONG");
    expect(screen.getByText("September").tagName).toBe("EM");
    expect(screen.getByText("4010").tagName).toBe("CODE");
    expect(screen.getByText("12,400.00 SGD").className).toContain("fy-fact");
    expect(screen.getByRole("link", { name: "the statement" }).getAttribute("href")).toBe("/reports/statements/pl");
    // a link outside Fyniq stays text, written out in full; the paragraph break makes two paragraphs
    expect(screen.queryByRole("link", { name: "a site" })).toBeNull();
    expect(screen.getByText(/\[a site\]\(https:\/\/example\.com\)/)).toBeTruthy();
    expect(screen.getByText("(I could not establish the November budget)")).toBeTruthy();
    expect(document.querySelectorAll("p.fy-ai-text")).toHaveLength(2);
    await noViolations(document.body);
  });
});
