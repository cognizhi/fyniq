import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { BrandPage } from "./Brand";

const options = ["Deep teal", "Navy", "Forest"].map((name, i) => ({ colour: `c-${i}`, name }));
const light = { media_type: "image/svg+xml", size_bytes: 6144, sha256: "a".repeat(64), cleaned: { elements: ["image", "script"], attributes: 1, external: 1 } };
const svg = btoa('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>');

// req: FR-ORG-09
describe("Logo and brand", () => {
  it("shows the logo, says what was cleaned, and axe finds nothing", async () => {
    answer({
      "GET /v1/organisation/brand": { status: 200, body: { version: 2, light, accent: "c-1", accent_options: options, updated_at: "2026-11-07T01:00:00Z" } },
      "GET /v1/organisation/brand/files/light": { status: 200, body: { kind: "light", version: 2, media_type: "image/svg+xml", sha256: light.sha256, content_base64: svg } },
    });
    renderPage(BrandPage, { path: "/settings/brand" });
    expect(await screen.findByAltText("For light backgrounds: your logo")).toBeTruthy();
    expect(screen.getByText(/We removed image, script, 1 unsafe attribute and 1 outside reference/)).toBeTruthy();
    expect(screen.getByRole("radio", { name: /Navy/ }).getAttribute("aria-checked")).toBe("true");
    await noViolations(document.body);
  });

  it("uploads a file at the brand's version, and sets an accent", async () => {
    const seen = answer({
      "GET /v1/organisation/brand": { status: 200, body: { version: 0, accent_options: options } },
      "PUT /v1/organisation/brand/files/dark": { status: 200, body: { version: 1, accent_options: options } },
      "PUT /v1/organisation/brand/accent": { status: 200, body: { version: 1, accent: "c-2", accent_options: options } },
    });
    renderPage(BrandPage, { path: "/settings/brand" });
    const darkSlot = await screen.findByText("For dark backgrounds");
    const input = darkSlot.closest("div.fy-stack")?.parentElement?.querySelector("input[type=file]") as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File(["<svg/>"], "dark.svg", { type: "image/svg+xml" })] } });
    await waitFor(() => expect(calls()).toContain("PUT /v1/organisation/brand/files/dark"));
    expect(seen.find((s) => s.path === "/v1/organisation/brand/files/dark")?.headers.get("If-Match")).toBe('"0"');
    fireEvent.click(within(screen.getByRole("radiogroup", { name: "Accent colour" })).getByRole("radio", { name: /Forest/ }));
    await waitFor(() => expect(seen.find((s) => s.path === "/v1/organisation/brand/accent")?.body).toEqual({ accent: "c-2" }));
  });
});
