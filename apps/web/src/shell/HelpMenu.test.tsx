import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { HelpMenu } from "./HelpMenu";

// req: REL-05
describe("the Help menu's About dialog", () => {
  it("shows the platform version and the commit from getPlatformVersion, and axe finds nothing", async () => {
    answer({
      "GET /v1/platform/version": {
        status: 200,
        body: { service: "api", version: "0.12.0", commit: "3f2c1a9d", build_time: "2026-11-11T02:00:00Z", flavour: "release", release: "v0.12.0" },
      },
    });
    renderPage(HelpMenu);
    fireEvent.click(await screen.findByRole("button", { name: "Help" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "About Fyniq" }));
    const about = await screen.findByRole("dialog", { name: "About Fyniq" });
    expect(await within(about).findByText("0.12.0")).toBeTruthy();
    expect(within(about).getByText("3f2c1a9d")).toBeTruthy();
    expect(within(about).getByText("v0.12.0")).toBeTruthy();
    expect(within(about).getByText("Release build")).toBeTruthy();
    expect(within(about).getByText(/Every exported report names them in its footer/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("says when the version cannot be read", async () => {
    answer({ "GET /v1/platform/version": { status: 503, body: { type: "about:blank", title: "Unavailable", status: 503, code: "API-UNAVAILABLE", detail: "Try again." } } });
    renderPage(HelpMenu);
    fireEvent.click(await screen.findByRole("button", { name: "Help" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "About Fyniq" }));
    expect(await screen.findByText("The version could not be read")).toBeTruthy();
  });
});
