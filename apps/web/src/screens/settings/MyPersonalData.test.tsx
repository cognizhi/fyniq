import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { MyPersonalData } from "./MyPersonalData";

const request = { id: "r-1", kind: "access", subject: "member:u-1", details: "A copy of my data.", received_on: "2026-11-01", due_on: "2026-12-01",
  status: "acknowledged", overdue: false, steps: [] };

// req: CMP-PD-04
describe("Settings › Profile › My personal data", () => {
  it("lists my requests and sends a correction, saying when it will be answered; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/me/personal-data-requests": { status: 200, body: { items: [request] } },
      "POST /v1/me/personal-data-requests": { status: 201, body: { ...request, id: "r-2", kind: "correction", status: "received", due_on: "2026-12-09" } },
    });
    renderPage(MyPersonalData, { path: "/settings/profile" });
    const list = await screen.findByRole("list", { name: "My requests" });
    expect(within(list).getByText(/To see my data: A copy of my data/)).toBeTruthy();
    await noViolations();
    fireEvent.click(screen.getByLabelText("Correct it"));
    fireEvent.change(screen.getByLabelText("What is wrong, and what it should say"), { target: { value: "My name is Ada Tan." } });
    fireEvent.click(screen.getByRole("button", { name: "Send the request" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST")).toBe(true));
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ kind: "correction", details: "My name is Ada Tan." });
    expect(await screen.findByText(/it will be answered by 9 Dec 2026/)).toBeTruthy();
  });
});
