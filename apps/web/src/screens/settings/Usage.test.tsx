import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Usage, sizeOf } from "./Usage";

const usage = {
  months: [
    { month: "2026-10", ai_calls: 120, input_tokens: 350000, output_tokens: 41000, ai_cost: [{ currency: "USD", amount: "12.34500000" }], exports: { files: 2, bytes: 2048 }, evidence: { files: 5, bytes: 5242880 } },
    { month: "2026-11", ai_calls: 0, input_tokens: 0, output_tokens: 0, ai_cost: [], exports: { files: 0, bytes: 0 }, evidence: { files: 0, bytes: 0 } },
  ],
  stored: { evidence: { files: 5, bytes: 5242880 }, captures: { files: 3, bytes: 3000 }, exports: { files: 2, bytes: 2048 }, total_bytes: 5247928 },
};

// req: FR-ADM-05
describe("Usage and cost", () => {
  it("shows the latest month first, the exact cost in its currency, and what is stored; axe finds nothing", async () => {
    answer({ "GET /v1/usage": { status: 200, body: usage } });
    renderPage(Usage, { path: "/settings/usage" });
    expect(await screen.findByRole("heading", { name: "By month" })).toBeTruthy();
    const rows = screen.getAllByRole("row");
    expect(rows[0]?.textContent).toContain("Kind"); // the stored table's head, then its rows
    expect(screen.getByText("USD 12.35")).toBeTruthy();
    expect(screen.getByText("None")).toBeTruthy();
    expect(screen.getByText("5.0 MB in all")).toBeTruthy();
    const months = screen.getAllByRole("rowheader").map((r) => r.textContent);
    expect(months.indexOf("Nov 2026")).toBeLessThan(months.indexOf("Oct 2026"));
    await noViolations(document.body);
  });

  it("writes sizes in the units a person reads", () => {
    expect([sizeOf(12), sizeOf(1536), sizeOf(5242880), sizeOf(3 * 1024 ** 3)]).toEqual(["12 B", "1.5 KB", "5.0 MB", "3.0 GB"]);
  });
});
