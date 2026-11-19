import { render, screen, waitFor } from "@testing-library/react";
import axe from "axe-core";
import { describe, expect, it } from "vitest";
import { App } from "./App";
import { ReviewBanner } from "./components/ReviewBanner";
import { Home } from "./pages/Home";
import { SignIn, signInURL } from "./pages/SignIn";
import { reviewsFor, review, type Register } from "./review";
import { SessionProvider } from "./session";
import { answer, calls } from "./test/setup";

const providers = { providers: [{ id: "local", name: "Local sign-in" }, { id: "google", name: "Google" }], setup_done: true };
const session = { landing: "/me", member_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45", organisation_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44", roles: ["employee"] };

async function violations(container: HTMLElement): Promise<string[]> {
  const r = await axe.run(container, { rules: { "color-contrast": { enabled: false } } }); // jsdom draws no colours: contrast is `make check-tokens`'s
  return r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`);
}

const app = () =>
  render(
    <SessionProvider>
      <App />
    </SessionProvider>,
  );

// req: NM-ARC-01, NM-UX-02
describe("sign-in", () => {
  it("offers each provider the API names as a navigation on the API, and no banner", async () => {
    answer("GET /v1/sign-in", { status: 200, body: providers });
    const { container } = render(<SignIn />);
    const link = await screen.findByRole("link", { name: /Continue with Google/ });
    expect(link.getAttribute("href")).toBe("/api/auth/signin?provider=google&return_to=%2F");
    expect(screen.queryByRole("note")).toBeNull();
    expect(await violations(container)).toEqual([]);
  });

  it("says Nemura is not set up before any provider, and when none is wired", async () => {
    answer("GET /v1/sign-in", { status: 200, body: { providers: [], setup_done: false } });
    const { unmount } = render(<SignIn />);
    expect(await screen.findByText(/not set up yet/)).toBeTruthy();
    expect(screen.queryByRole("link", { name: /Continue/ })).toBeNull();
    unmount();
    answer("GET /v1/sign-in", { status: 200, body: { providers: null, setup_done: true } });
    render(<SignIn />);
    expect(await screen.findByText(/No way to sign in/)).toBeTruthy();
  });

  it("says what went wrong when the API cannot be reached, and tries again on request", async () => {
    render(<SignIn />);
    expect(await screen.findByRole("alert")).toBeTruthy();
    answer("GET /v1/sign-in", { status: 200, body: providers });
    screen.getByRole("button", { name: "Try again" }).click();
    expect(await screen.findByRole("link", { name: /Continue with Local sign-in/ })).toBeTruthy();
  });

  it("only ever sends the browser back to a path on this origin, encoded", () => {
    expect(signInURL("a b", "/me?x=1&y=2")).toBe("/api/auth/signin?provider=a%20b&return_to=%2Fme%3Fx%3D1%26y%3D2");
  });
});

// req: NM-ROLE-02, NM-UX-02
describe("the app", () => {
  it("sends a signed-out person to sign-in, and a signed-in one to their landing", async () => {
    answer("GET /v1/sign-in", { status: 200, body: providers });
    window.history.replaceState(null, "", "/people");
    const out = app();
    await waitFor(() => expect(window.location.pathname).toBe("/sign-in"));
    out.unmount();

    answer("GET /v1/session", { status: 200, body: session });
    window.history.replaceState(null, "", "/");
    const inn = app();
    await waitFor(() => expect(window.location.pathname).toBe("/me"));
    expect(await screen.findByRole("heading", { name: "My home" })).toBeTruthy();
    expect(await violations(inn.container)).toEqual([]);
    expect(calls).toContain("GET /v1/session");
  });

  it("lands the people team on People home, and never shows a signed-in person sign-in", async () => {
    answer("GET /v1/session", { status: 200, body: { ...session, landing: "/people", roles: ["hr_admin"] } });
    window.history.replaceState(null, "", "/sign-in");
    app();
    await waitFor(() => expect(window.location.pathname).toBe("/people"));
    expect(await screen.findByRole("heading", { name: "People home" })).toBeTruthy();
    expect(screen.getByText("HR administrator")).toBeTruthy();
  });

  it("names a page that does not exist and goes home", async () => {
    answer("GET /v1/session", { status: 200, body: session });
    window.history.replaceState(null, "", "/nowhere");
    app();
    expect(await screen.findByRole("heading", { name: /nothing at this address/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Go to my home" }).getAttribute("href")).toBe("/me");
  });

  it("signs out with a form post on the API, so no script holds the session", async () => {
    render(<Home session={session} screen="me" />);
    const form = screen.getByRole("button", { name: "Sign out" }).closest("form");
    expect(form?.getAttribute("method")).toBe("post");
    expect(form?.getAttribute("action")).toBe("/api/auth/signout");
  });
});

// req: NM-UX-06
describe("the Needs additional verification banner", () => {
  const register: Register = {
    heading: "Needs additional verification",
    help_key: "review.awaiting-verification",
    never: ["nm-sign-in", "nm-my-home"],
    areas: [
      { id: "payroll", status: "pending", short: "Payroll rules await review", message: "Check the figures.", screens: ["nm-payroll-hr", "nm-my-home", "nm-sign-in"] },
      { id: "done", status: "reviewed", short: "Done", message: "Done.", screens: ["nm-checks"] },
    ],
  };

  it("shows on a listed page while its area is pending, never on a page the register excludes or an area it has reviewed", () => {
    expect(reviewsFor("nm-payroll-hr", register).map((a) => a.id)).toEqual(["payroll"]);
    expect(reviewsFor("nm-checks", register)).toEqual([]);
    expect(reviewsFor("nm-my-home", register)).toEqual([]); // self-service never carries it, even when an area lists it
    expect(reviewsFor("nm-sign-in", register)).toEqual([]);
    const { container } = render(<ReviewBanner screen="nm-payroll-hr" register={register} />);
    expect(screen.getByRole("note").textContent).toContain("Payroll rules await review. Check the figures.");
    expect(container.querySelector("[title]")?.getAttribute("title")).toMatch(/second review/);
  });

  it("is on none of the self-service pages or sign-in in the real register", () => {
    for (const screen of ["nm-sign-in", "nm-my-home", "nm-my-pay", "nm-my-leave", "nm-ask"]) expect(reviewsFor(screen, review), screen).toEqual([]);
  });

  it("renders nothing when nothing awaits review", () => {
    const { container } = render(<ReviewBanner screen="nm-checks" register={register} />);
    expect(container.innerHTML).toBe("");
  });
});
