import { cleanup, render, screen, waitFor } from "@testing-library/react";
import axe from "axe-core";
import { describe, expect, it } from "vitest";
import { App } from "./App";
import { ReviewBanner } from "./components/ReviewBanner";
import { Home } from "./pages/Home";
import { Features } from "./pages/Features";
import { Directory } from "./pages/Directory";
import { Join } from "./pages/Join";
import { Person } from "./pages/Person";
import { matchRoute } from "./registry";
import { SignIn, signInURL } from "./pages/SignIn";
import { reviewsFor, review, type Register } from "./review";
import { SessionProvider } from "./session";
import { answer, calls } from "./test/setup";

const providers = { providers: [{ id: "local", name: "Local sign-in" }, { id: "google", name: "Google" }], setup_done: true };
const session = { landing: "/me", member_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45", organisation_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44", roles: ["employee"] };

async function userType(input: HTMLInputElement, text: string): Promise<void> {
  const { fireEvent } = await import("@testing-library/react");
  fireEvent.change(input, { target: { value: text } });
}

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

// req: NM-ARC-01
describe("joining with a code", () => {
  it("posts the setup form to the API with a hidden code, the organisation and the person's name", async () => {
    answer("GET /v1/sign-in", { status: 200, body: { providers: [{ id: "local", name: "Local sign-in" }], setup_done: false } });
    const { container } = render(<Join kind="setup" />);
    await screen.findByRole("option", { name: "Local sign-in" });
    const form = container.querySelector("form");
    expect(form?.getAttribute("method")).toBe("post");
    expect(form?.getAttribute("action")).toBe("/api/auth/join");
    expect(container.querySelector("input[name=kind]")?.getAttribute("value")).toBe("setup");
    expect(container.querySelector("input[name=code]")?.getAttribute("type")).toBe("password");
    for (const f of ["organisation", "name"]) expect(container.querySelector(`input[name=${f}]`), f).not.toBeNull();
    expect(await violations(container)).toEqual([]);
  });

  it("asks an invitation's holder for the code alone", async () => {
    answer("GET /v1/sign-in", { status: 200, body: providers });
    const { container } = render(<Join kind="invitation" />);
    await screen.findByRole("option", { name: "Google" });
    expect(container.querySelector("input[name=kind]")?.getAttribute("value")).toBe("invitation");
    expect(container.querySelector("input[name=organisation]")).toBeNull();
    expect(await violations(container)).toEqual([]);
  });

  it("asks a recovered Owner for the recovery code alone, and sign-in links to it", async () => {
    answer("GET /v1/sign-in", { status: 200, body: providers });
    const { container } = render(<Join kind="recovery" />);
    await screen.findByRole("option", { name: "Google" });
    expect(container.querySelector("input[name=kind]")?.getAttribute("value")).toBe("recovery");
    expect(container.querySelector("input[name=organisation]")).toBeNull();
    expect(screen.getByRole("heading", { name: /Recover an Owner/ })).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    cleanup();
    render(<SignIn />);
    expect((await screen.findByRole("link", { name: "I have a recovery code" })).getAttribute("href")).toBe("/join?kind=recovery");
  });

  it("says no way to sign in is set up when no provider is", async () => {
    answer("GET /v1/sign-in", { status: 200, body: { providers: [], setup_done: true } });
    render(<Join kind="invitation" />);
    expect(await screen.findByText(/No way to sign in/)).toBeTruthy();
  });

  it("explains a refused sign-in in plain words from its code, and says nothing for a code it does not know", async () => {
    answer("GET /v1/sign-in", { status: 200, body: providers });
    window.history.replaceState(null, "", "/sign-in?signin_error=NM_SIGNIN_08");
    const { unmount } = render(<SignIn />);
    expect((await screen.findByRole("alert")).textContent).toMatch(/wrong, was used already or has expired/);
    unmount();
    window.history.replaceState(null, "", "/sign-in?signin_error=<script>");
    render(<SignIn />);
    await screen.findByRole("link", { name: /Continue with Google/ });
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("links sign-in to both ways in, and setup only before setup is done", async () => {
    answer("GET /v1/sign-in", { status: 200, body: { providers: [], setup_done: false } });
    const { unmount } = render(<SignIn />);
    expect((await screen.findByRole("link", { name: "Set up Nemura" })).getAttribute("href")).toBe("/join?kind=setup");
    expect(screen.getByRole("link", { name: "I have an invitation" }).getAttribute("href")).toBe("/join?kind=invitation");
    unmount();
    answer("GET /v1/sign-in", { status: 200, body: providers });
    render(<SignIn />);
    await screen.findByRole("link", { name: /Continue with Google/ });
    expect(screen.queryByRole("link", { name: "Set up Nemura" })).toBeNull();
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

// req: NM-PPL-06, NM-PPL-01, NM-PPL-08
describe("the people pages", () => {
  const ana = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", name: "Ana Tan", job_title: "CEO", department: "Management", work_email: "ana@acme.example" };
  const ben = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e62", name: "Ben Lim", preferred_name: "Benny", job_title: "Engineer", department: "Engineering", manager_id: ana.id, manager_name: "Ana Tan" };

  it("lists everyone, filters by any word, and links a name only for the people team", async () => {
    answer("GET /v1/people", { status: 200, body: { people: [ana, ben] } });
    const { container, unmount } = render(<Directory roles={["employee"]} />);
    await screen.findByText("Ben Lim");
    expect(screen.queryByRole("link", { name: "Ben Lim" })).toBeNull();
    expect(await violations(container)).toEqual([]);
    unmount();
    render(<Directory roles={["hr_officer"]} />);
    expect((await screen.findByRole("link", { name: "Ben Lim" })).getAttribute("href")).toBe(`/people/${ben.id}`);
    const filter = screen.getByLabelText("Find a person") as HTMLInputElement;
    await userType(filter, "engin");
    expect(screen.queryByRole("link", { name: "Ana Tan" })).toBeNull();
    expect(screen.getByText("Ben Lim")).toBeTruthy();
    await userType(filter, "nobody");
    expect(await screen.findByText("No one matches that.")).toBeTruthy();
  });

  it("says so when nobody works here, and when the API cannot be reached", async () => {
    answer("GET /v1/people", { status: 200, body: { people: [] } });
    const { unmount } = render(<Directory roles={["hr_admin"]} />);
    expect(await screen.findByText(/Nobody works here yet/)).toBeTruthy();
    unmount();
    answer("GET /v1/people", { status: 500, body: { code: "X" } });
    render(<Directory roles={["hr_admin"]} />);
    expect(await screen.findByRole("alert")).toBeTruthy();
  });

  it("shows a record with its job history and, only when the API sends it, the classification", async () => {
    const rec = { id: ben.id, legal_name: "Ben Lim", preferred_name: "Benny", work_email: "ben@acme.example", employment_start: "2026-02-01", has_login: true,
      jobs: [{ effective_from: "2026-02-01", effective_to: "2026-04-01", job_title: "Engineer", department: "Engineering", worksite: "SG", employment_type: "full_time" }, { effective_from: "2026-04-01", job_title: "Senior Engineer", worksite: "SG", employment_type: "full_time" }] };
    answer(`GET /v1/people/${ben.id}`, { status: 200, body: { ...rec, classification: { contract_type: "service", ea_covered: true, part_iv_class: "none", residency: "permanent_resident", pr_start_date: "2090-01-01", birth_date: "1990-05-06" } } });
    const { container, unmount } = render(<Person id={ben.id} roles={["hr_admin"]} />);
    expect(await screen.findByRole("heading", { name: "Ben Lim (Benny)" })).toBeTruthy();
    expect(screen.getAllByRole("row")).toHaveLength(3);
    expect(screen.getByText("Current")).toBeTruthy();
    expect(screen.getByText("Permanent resident")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    unmount();
    answer(`GET /v1/people/${ben.id}`, { status: 200, body: rec });
    render(<Person id={ben.id} roles={["line_manager"]} />);
    await screen.findByRole("heading", { name: "Ben Lim (Benny)" });
    expect(screen.queryByText("Legal classification")).toBeNull();
    expect(screen.queryByText("Date of birth")).toBeNull();
  });

  it("does not say whether a person exists when the record is refused", async () => {
    answer(`GET /v1/people/${ben.id}`, { status: 404, body: { code: "NM_PERSON_NOT_FOUND" } });
    render(<Person id={ben.id} roles={["employee"]} />);
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toMatch(/no such person, or you may not see/);
  });

  it("finds the route of a path, the exact one before a parameter", () => {
    expect(matchRoute("/people/directory")?.route.id).toBe("people-directory");
    expect(matchRoute("/people")?.route.id).toBe("people-home");
    expect(matchRoute(`/people/${ben.id}`)?.params.id).toBe(ben.id);
    expect(matchRoute("/people/a/b")).toBeUndefined();
    expect(matchRoute("/nowhere")).toBeUndefined();
  });
});

// req: NM-FEAT-01, NM-FEAT-03
describe("the feature switches", () => {
  const list = { features: [
    { id: "leave", name: "Leave", phase: "P1", state: "on", needs: [] },
    { id: "payroll", name: "Payroll", phase: "P1", state: "off", needs: ["leave"] },
    { id: "hiring", name: "Hiring", phase: "P2", state: "off", needs: [] },
  ] };

  it("lists each feature with its state and what it needs, and says which are later", async () => {
    answer("GET /v1/features", { status: 200, body: list });
    const { container } = render(<Features roles={["hr_admin"]} />);
    await screen.findByText("Payroll");
    expect(screen.getByText(/Built in P2/)).toBeTruthy();
    expect(screen.getAllByRole("combobox")).toHaveLength(3);
    expect(await violations(container)).toEqual([]);
  });

  it("shows the API's plain answer when a switch is refused, and applies one that is allowed", async () => {
    answer("GET /v1/features", { status: 200, body: list });
    answer("PUT /v1/features/payroll", { status: 409, body: { code: "NM_FEATURE_UNAVAILABLE", detail: "Payroll needs the Owner and a second approver to switch on." } });
    render(<Features roles={["owner"]} />);
    await screen.findByText("Payroll");
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Payroll: State"), { target: { value: "on" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Apply" })[1] as HTMLElement);
    expect((await screen.findByRole("alert")).textContent).toMatch(/second approver/);
    answer("PUT /v1/features/hiring", { status: 204 });
    fireEvent.change(screen.getByLabelText("Hiring: State"), { target: { value: "external" } });
    fireEvent.change(screen.getByLabelText("Hiring: System that holds the data"), { target: { value: "Acme HR" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Apply" })[2] as HTMLElement);
    await waitFor(() => expect(calls).toContain("PUT /v1/features/hiring"));
  });

  it("is read-only for someone who may only look, and says so when the API refuses the list", async () => {
    answer("GET /v1/features", { status: 200, body: list });
    const { unmount } = render(<Features roles={["hr_officer"]} />);
    await screen.findByText("Payroll");
    expect(screen.queryByRole("button", { name: "Apply" })).toBeNull();
    for (const box of screen.getAllByRole("combobox")) expect((box as HTMLSelectElement).disabled).toBe(true);
    unmount();
    answer("GET /v1/features", { status: 403, body: { code: "NM_ROLE_REQUIRED" } });
    render(<Features roles={["employee"]} />);
    expect(await screen.findByRole("alert")).toBeTruthy();
  });
});
