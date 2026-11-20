import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { NemuraPasskeyReminder } from "./NemuraPasskeyReminder";

// part of FR-PPL-09 (the approver is asked beforehand to enrol the passkey Nemura's approval is signed with)
describe("NemuraPasskeyReminder", () => {
  it("shows nothing unless Nemura said this person approves pay runs and they have no passkey", () => {
    renderPage(() => <NemuraPasskeyReminder needed={false} />);
    expect(screen.queryByRole("note")).toBeNull();
  });
  it("asks for a passkey, says it grants no role, and links to where one is added", async () => {
    renderPage(() => <NemuraPasskeyReminder needed />);
    expect(await screen.findByText("Add a passkey to approve pay runs in Nemura")).toBeTruthy();
    expect(screen.getByText(/gives you no role in Fyniq/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Add a passkey" }).getAttribute("href")).toBe("/sign-in/enrol");
    await noViolations(document.body);
  });
});
