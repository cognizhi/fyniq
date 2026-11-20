import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { WorkspaceSwitch } from "./shell/WorkspaceSwitch";
import { rememberPage, switchedIn } from "./workspace";

// req: FR-PPL-02
describe("the Finance | People switch", () => {
  beforeEach(() => window.localStorage.clear());

  it("shows only when the session names where People is, and says Finance is here", () => {
    const { container, rerender } = render(<WorkspaceSwitch session={{ workspace: { people_url: "https://people.example.test/?switch=1" } }} />);
    expect(screen.getByRole("link", { name: "People" }).getAttribute("href")).toBe("https://people.example.test/?switch=1");
    expect(screen.getByText("Finance").getAttribute("aria-current")).toBe("page");
    rerender(<WorkspaceSwitch session={{}} />);
    expect(container.querySelector("nav")).toBeNull();
  });

  it("brings a person back to the page they left, and never anywhere but a page of this origin", () => {
    expect(switchedIn("?switch=1", "/")).toBeNull();
    rememberPage("/sales/invoices");
    expect(switchedIn("?switch=1", "/")).toBe("/sales/invoices");
    expect(switchedIn("", "/")).toBeNull();
    expect(switchedIn("?switch=1", "/banking")).toBeNull();
    rememberPage("//evil.example.test");
    rememberPage("/sign-in");
    expect(switchedIn("?switch=1", "/")).toBe("/sales/invoices");
    window.localStorage.setItem("fyniq.lastPage", "https://evil.example.test/");
    expect(switchedIn("?switch=1", "/")).toBeNull();
  });
});

// part of FR-PPL-03 (the provider the other app named is only a hint of the shape a provider has)
describe("switchProvider", () => {
  it("takes only an ID of the shape a provider has", async () => {
    const { switchProvider } = await import("./route");
    expect(switchProvider("?switch=1&provider=google")).toBe("google");
    expect(switchProvider("?provider=zoho-sso")).toBe("zoho-sso");
    for (const bad of ["", "?switch=1", "?provider=", "?provider=Google", "?provider=a%2Fb", "?provider=%2F%2Fevil.example", "?provider=../x", "?provider=" + "a".repeat(64)]) {
      expect(switchProvider(bad)).toBeNull();
    }
  });
});
