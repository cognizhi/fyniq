import { cleanup, render, screen, waitFor } from "@testing-library/react";
import axe from "axe-core";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import { ReviewBanner } from "./components/ReviewBanner";
import { Home } from "./pages/Home";
import { NewPerson } from "./pages/NewPerson";
import { ApplyLeave } from "./pages/ApplyLeave";
import { GovernmentLeave } from "./pages/GovernmentLeave";
import { MyLeave } from "./pages/MyLeave";
import { LetterTemplates } from "./pages/LetterTemplates";
import { Letters } from "./pages/Letters";
import { RequestLetter } from "./pages/RequestLetter";
import { PeopleTiles } from "./components/PeopleTiles";
import { MyRequests } from "./pages/MyRequests";
import { ReportSick } from "./pages/ReportSick";
import { TeamLeave } from "./pages/TeamLeave";
import { Packs } from "./pages/Packs";
import { Policy } from "./pages/Policy";
import { Features } from "./pages/Features";
import { Directory } from "./pages/Directory";
import { Join } from "./pages/Join";
import { Person } from "./pages/Person";
import { TermsPanel } from "./components/TermsPanel";
import { MyDetails } from "./pages/MyDetails";
import { LeaverHome } from "./pages/LeaverHome";
import { PayRunApproval } from "./pages/PayRunApproval";
import { MyPay, Payslip } from "./pages/MyPay";
import { PayRuns } from "./components/PayRuns";
import { PayrollHR } from "./pages/PayrollHR";
import { PeopleOnboarding } from "./pages/PeopleOnboarding";
import { Delegations } from "./components/Delegations";
import { AskQuestion } from "./pages/AskQuestion";
import { PeopleRequests } from "./pages/PeopleRequests";
import { RequestSteps } from "./components/RequestSteps";
import { matchRoute } from "./registry";
import { SignIn, signInURL } from "./pages/SignIn";
import { reviewsFor, review, type Register } from "./review";
import { SessionProvider } from "./session";
import { answer, bodies, calls } from "./test/setup";

const stepUp = vi.hoisted(() => ({ result: undefined as string | undefined }));
vi.mock("./api/stepup", () => ({ confirmWithPasskey: async () => stepUp.result }));

const providers = { providers: [{ id: "local", name: "Local sign-in" }, { id: "google", name: "Google" }], setup_done: true };
const session = { landing: "/me", member_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45", organisation_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44", roles: ["employee"], leaver: false };

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

// req: NM-PPL-01
describe("adding a person", () => {
  it("sends the form with a new ID and the first job, goes to the record, and shows the API's refusal in plain words", async () => {
    answer("GET /v1/people", { status: 200, body: { people: [{ id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e62", name: "Ana Tan" }] } });
    answer("POST /v1/people", { status: 422, body: { code: "NM_PERSON_INVALID", detail: "The legal name is required: enter it." } });
    const { container } = render(<NewPerson roles={["hr_officer"]} />);
    await screen.findByRole("option", { name: "Ana Tan" });
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Legal name"), { target: { value: "Ben Lim" } });
    fireEvent.change(screen.getByLabelText("Start date"), { target: { value: "2026-02-01" } });
    fireEvent.change(screen.getByLabelText("Job title"), { target: { value: "Engineer" } });
    fireEvent.submit(container.querySelector("form.fy-stack") as HTMLFormElement);
    expect((await screen.findByRole("alert")).textContent).toMatch(/legal name is required/);
    answer("POST /v1/people", { status: 201 });
    fireEvent.submit(container.querySelector("form.fy-stack") as HTMLFormElement);
    await waitFor(() => expect(window.location.pathname).toMatch(/^\/people\/[0-9a-f-]{36}$/));
  });

  it("says who may add people, and disables the button for anyone else", async () => {
    answer("GET /v1/people", { status: 200, body: { people: [] } });
    render(<NewPerson roles={["employee"]} />);
    expect(screen.getByRole("alert")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Add person" }) as HTMLButtonElement).disabled).toBe(true);
  });
});

// req: NM-PACK-02, NM-PACK-04, NM-PACK-07
describe("the country pack and the leave policy", () => {
  const pack = { packs: [{ id: "sg-people@2026.1", title: "Singapore people facts", effective_from: "2026-01-01", review_date: "2027-01-31", verifier: "none yet", accepted: false, researched_values: 2,
    values: [{ table: "annual_leave", key: "1", status: "researched", decided: false }, { table: "timing", key: "salary_days_after_period", status: "researched", decided: true }],
    sources: [{ id: "mom-paying-salary", title: "MOM: Paying salary", url: "https://www.mom.gov.sg/employment-practices/salary/paying-salary", section: "When to pay salary", retrieved: "2026-11-19", quote: "Within 7 days after the end of the salary period." }] }] };

  it("shows each version's sources and researched values, and records a decision with a reason", async () => {
    answer("GET /v1/packs", { status: 200, body: pack });
    answer("POST /v1/packs/sg-people%402026.1/decisions", { status: 201 });
    const { container } = render(<Packs roles={["hr_admin"]} />);
    await screen.findByText("sg-people@2026.1");
    expect(screen.getByText(/Within 7 days after the end/)).toBeTruthy();
    expect(screen.getByText("Decided")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("annual_leave/1: Why your company uses it"), { target: { value: "We read MOM's page and it matches." } });
    fireEvent.click(screen.getByRole("button", { name: "Record decision" }));
    await waitFor(() => expect(calls).toContain("POST /v1/packs/sg-people%402026.1/decisions"));
  });

  it("offers no change to someone who may only read", async () => {
    answer("GET /v1/packs", { status: 200, body: pack });
    render(<Packs roles={["hr_officer"]} />);
    await screen.findByText("sg-people@2026.1");
    expect(screen.queryByRole("button", { name: "Record decision" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Accept this version" })).toBeNull();
  });

  it("sets a policy and shows the API's plain refusal naming each rule", async () => {
    answer("GET /v1/policy", { status: 200, body: { in_force: false, rules: { annual_leave_days: [], sick_outpatient_days: [], sick_hospital_days: [], childcare_citizen_days: 0, extended_childcare_days: 0, childcare_non_citizen_days: 0 } } });
    answer("GET /v1/checks", { status: 200, body: { findings: [{ check: 13, area: "Packs", status: "fail", subject: "sg-people@2026.1", message: "The company policy needs a researched value with no decision." }] } });
    answer("PUT /v1/policy", { status: 422, body: { code: "NM_POLICY_BELOW_STATUTE", detail: "Annual leave in year 1 of service gives 6, below the statutory 7." } });
    const { container } = render(<Policy roles={["hr_admin"]} />);
    expect(await screen.findByText(/needs a researched value/)).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Starts on"), { target: { value: "2026-12-01" } });
    fireEvent.change(screen.getByLabelText("Annual leave days by year of service"), { target: { value: "6" } });
    fireEvent.change(screen.getByLabelText("Outpatient sick leave days at 3, 4, 5 and 6 months"), { target: { value: "5, 8, 11, 14" } });
    fireEvent.change(screen.getByLabelText("Hospitalisation leave days at 3, 4, 5 and 6 months"), { target: { value: "15, 30, 45, 60" } });
    for (const [label, v] of [["Childcare leave, citizen child under 7", "6"], ["Extended childcare leave, citizen child 7 to 12", "2"], ["Childcare leave, non-citizen child", "2"]]) fireEvent.change(screen.getByLabelText(label as string), { target: { value: v } });
    fireEvent.submit(container.querySelector("form.fy-stack") as HTMLFormElement);
    expect((await screen.findByRole("alert")).textContent).toMatch(/below the statutory 7/);
  });
});

// req: NM-LV-02, NM-TIM-01, NM-LV-13
const emptyCalendar = () => answer("GET /v1/leave-calendar", { status: 200, body: { month: "2026-05", people: [] } });

describe("my leave", () => {
  beforeEach(emptyCalendar);
  const view = { person_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", year: 2026, working_pattern: { effective_from: "2026-01-05", weekdays: [1, 2, 3, 4, 5], hours_per_week: "44.0" },
    types: [{ key: "annual", name: "Annual leave", counting: "working_days", balance: { entitled: "7.00", taken: "2.00", booked: "1.00", expired: "0.00", encashed: "0.00", left: "4.00" },
      basis: ["Year of service 1: 7 days a year, 12 completed months of 12."], sources: ["sg-people@2026.2 annual_leave"], carried_until: "2027-12-31" }] };
  const year = String(new Date().getFullYear());

  it("shows the balance with the steps of the calculation and where the days come from", async () => {
    answer(`GET /v1/me/leave/${year}`, { status: 200, body: { ...view, year: Number(year) } });
    const { container } = render(<MyLeave roles={["employee"]} />);
    expect(await screen.findByText("Year of service 1: 7 days a year, 12 completed months of 12.")).toBeTruthy();
    expect(screen.getByRole("table", { name: "Balance of Annual leave" }).textContent).toContain("4.00");
    expect(screen.getByText(/From 2026-01-05: Monday, Tuesday, Wednesday, Thursday, Friday, 44.0 hours a week/)).toBeTruthy();
    expect(screen.getByText(/Where the days come from: sg-people@2026.2 annual_leave/)).toBeTruthy();
    expect(screen.getByText(/can be taken until 2027-12-31/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Work out annual leave" })).toBeNull();
    expect(await violations(container)).toEqual([]);
  });

  it("says plainly that leave is off, and moves between years", async () => {
    answer(`GET /v1/me/leave/${year}`, { status: 409, body: { code: "NM_FEATURE_OFF", detail: "Leave is switched off for this organisation: an Owner switches it on." } });
    render(<MyLeave roles={["employee"]} />);
    expect((await screen.findByRole("alert")).textContent).toMatch(/switched off/);
    const { fireEvent } = await import("@testing-library/react");
    answer(`GET /v1/me/leave/${Number(year) - 1}`, { status: 200, body: { ...view, year: Number(year) - 1, types: [] } });
    fireEvent.click(screen.getByRole("button", { name: "Previous year" }));
    expect(await screen.findByText("No leave has been granted for this year yet.")).toBeTruthy();
  });

  it("lets the people team work out a year, carry days forward and set the pattern, and shows a refusal in words", async () => {
    const id = view.person_id;
    answer(`GET /v1/people/${id}/leave/${year}`, { status: 200, body: { ...view, year: Number(year) } });
    answer(`POST /v1/people/${id}/leave/${year}/annual-grant`, { status: 200, body: { days: "7.00", written: "7.00", basis: [], sources: [] } });
    answer(`POST /v1/people/${id}/leave/${year}/carry-forward`, { status: 200, body: { carried: "4.00" } });
    answer(`PUT /v1/people/${id}/working-pattern`, { status: 409, body: { code: "NM_LEAVE_CONFLICT", detail: "The person already has a working pattern on those days: start the new one on or after the day the last one ends." } });
    const { container } = render(<MyLeave roles={["hr_admin"]} personId={id} />);
    await screen.findByRole("button", { name: "Work out annual leave" });
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getByRole("button", { name: "Work out annual leave" }));
    expect(await screen.findByText("Recorded 7.00 days for the year.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Carry unused days forward" }));
    expect(await screen.findByText("Carried 4.00 days into the next year.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Starts on"), { target: { value: "2026-01-05" } });
    fireEvent.submit(container.querySelector("form.fy-stack[aria-labelledby='pattern-form-h']") as HTMLFormElement);
    expect((await screen.findByRole("alert")).textContent).toMatch(/already has a working pattern/);
    expect(calls).toContain(`PUT /v1/people/${id}/working-pattern`);
  });

  it("offers an employee no way to change anyone's leave", async () => {
    const id = view.person_id;
    answer(`GET /v1/people/${id}/leave/${year}`, { status: 200, body: { ...view, year: Number(year) } });
    render(<MyLeave roles={["manager"]} personId={id} />);
    await screen.findByText("Annual leave");
    expect(screen.queryByRole("button", { name: "Carry unused days forward" })).toBeNull();
  });

  it("is routed from the address", () => {
    expect(matchRoute("/me/leave")?.route.id).toBe("my-leave");
    expect(matchRoute("/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61/leave")?.route.id).toBe("person-leave");
    expect(matchRoute("/people/directory")?.route.id).toBe("people-directory");
  });
});

// req: NM-LV-03, NM-LV-04, NM-LV-05
describe("applying for and approving leave", () => {
  beforeEach(emptyCalendar);
  const preview = { days: "4.00", left_after: "10.00", enough: true, counting: "working_days",
    days_counted: [{ date: "2026-04-01", cost: "1.00" }, { date: "2026-04-03", cost: "0.00", why: "Good Friday" }, { date: "2026-04-04", cost: "0.00", why: "a rest day" }],
    away: [{ name: "Ben Lim", from: "2026-04-06", to: "2026-04-08" }] };

  it("shows the cost day by day, the balance after it and who is away, then sends the request", async () => {
    answer("POST /v1/me/leave-requests/preview", { status: 200, body: preview });
    answer("POST /v1/me/leave-requests", { status: 201, body: { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e80", days: "4.00" } });
    const { container } = render(<ApplyLeave roles={["employee"]} />);
    const { fireEvent } = await import("@testing-library/react");
    expect(screen.getByRole("button", { name: "Send to my manager" }).hasAttribute("disabled")).toBe(true);
    fireEvent.change(screen.getByLabelText("First day"), { target: { value: "2026-04-01" } });
    expect(await screen.findByText("This request costs 4.00 days. After it you have 10.00 days left.")).toBeTruthy();
    expect(screen.getByText("Good Friday")).toBeTruthy();
    expect(screen.getByText("Ben Lim: 2026-04-06 to 2026-04-08")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    fireEvent.click(screen.getByRole("button", { name: "Send to my manager" }));
    expect(await screen.findByText(/Sent\. Your manager has been told, and 4\.00 days are reserved/)).toBeTruthy();
    expect(calls).toContain("POST /v1/me/leave-requests");
  });

  it("refuses to send a request beyond the balance and says so", async () => {
    answer("POST /v1/me/leave-requests/preview", { status: 200, body: { ...preview, enough: false, left_after: "-2.00" } });
    render(<ApplyLeave roles={["employee"]} />);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("First day"), { target: { value: "2026-04-01" } });
    expect((await screen.findByRole("alert")).textContent).toMatch(/more than your balance: after it you would have -2\.00 days/);
    expect(screen.getByRole("button", { name: "Send to my manager" }).hasAttribute("disabled")).toBe(true);
  });

  it("shows the API's refusal in words, such as a missing working pattern", async () => {
    answer("POST /v1/me/leave-requests/preview", { status: 422, body: { code: "NM_LEAVE_INVALID", detail: "No working pattern is set for you on the first day: ask the people team to set it, so your days can be counted." } });
    render(<ApplyLeave roles={["employee"]} />);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("First day"), { target: { value: "2026-04-01" } });
    expect((await screen.findByRole("alert")).textContent).toMatch(/No working pattern is set/);
  });

  const pending = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e80", person_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", person_name: "Ben Lim", type: "annual", from: "2026-04-06", to: "2026-04-08", start_half: false, end_half: false, days: "3.00", note: "Family", state: "pending", requested_at: "2026-03-30T09:00:00Z" };
  const refused = { ...pending, id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e81", person_name: "Cy Wong", state: "refused", decision_reason: "The release freezes that week." };

  it("lists what waits and what was decided, approves, and needs a reason to refuse", async () => {
    answer("GET /v1/leave-requests", { status: 200, body: { requests: [pending, refused] } });
    answer(`POST /v1/leave-requests/${pending.id}/decision`, { status: 204 });
    const { container } = render(<TeamLeave roles={["line_manager"]} />);
    expect(await screen.findByText("Ben Lim")).toBeTruthy();
    expect(screen.getByText("The release freezes that week.")).toBeTruthy();
    expect(screen.getByText("Refused")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    answer(`POST /v1/leave-requests/${pending.id}/decision`, { status: 422, body: { code: "NM_LEAVE_INVALID", detail: "A refusal needs a reason of up to 500 characters: tell the person why." } });
    fireEvent.click(screen.getByRole("button", { name: "Refuse" }));
    expect((await screen.findByRole("alert")).textContent).toMatch(/needs a reason/);
    answer(`POST /v1/leave-requests/${pending.id}/decision`, { status: 204 });
    fireEvent.change(screen.getByLabelText("Reason for refusing Ben Lim"), { target: { value: "Too many away." } });
    fireEvent.click(screen.getByRole("button", { name: "Refuse" }));
    expect(await screen.findByText("Refused, and the person is told why.")).toBeTruthy();
  });

  it("is routed from the address, and the team link is for approvers", () => {
    expect(matchRoute("/me/leave/new")?.route.id).toBe("apply-leave");
    expect(matchRoute("/people/leave")?.route.id).toBe("team-leave");
  });
});

// req: NM-LV-12, NM-LV-06
describe("telling the company you are sick", () => {
  const notice = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90", days: "3.00", paid_days: "3.00", unpaid_days: "0.00", late: false, person_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", person_name: "Ben Lim",
    kind: "outpatient", from: "2026-05-04", to: "2026-05-06", has_certificate: false, verified: false };

  it("tells the company, shows the paid and unpaid days and flags a late notice", async () => {
    answer("GET /v1/sick-leave", { status: 200, body: { notifications: [] } });
    answer("POST /v1/me/sick-leave", { status: 201, body: { id: notice.id, days: "10.00", paid_days: "5.00", unpaid_days: "5.00", late: true } });
    const { container } = render(<ReportSick roles={["employee"]} />);
    await screen.findByText("You have not told the company about any sick leave.");
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("First day"), { target: { value: "2026-05-11" } });
    fireEvent.submit(container.querySelector("form.fy-stack") as HTMLFormElement);
    expect(await screen.findByText(/10\.00 working days: 5\.00 are paid sick leave and 5\.00 are beyond your limit/)).toBeTruthy();
    expect(screen.getByText(/marked late/)).toBeTruthy();
  });

  it("gives the certificate once, then shows who issued it and that it waits for verification", async () => {
    answer("GET /v1/sick-leave", { status: 200, body: { notifications: [notice] } });
    answer(`POST /v1/sick-leave/${notice.id}/certificate`, { status: 201 });
    render(<ReportSick roles={["employee"]} />);
    await screen.findByText("2026-05-04 to 2026-05-06: 3.00 days, 3.00 paid");
    const { fireEvent, waitFor: wait } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Who issued it"), { target: { value: "Raffles Medical Clinic" } });
    const file = new File(["jpeg bytes"], "mc.jpg", { type: "image/jpeg" });
    fireEvent.change(screen.getByLabelText("Photo or e-MC file"), { target: { files: [file] } });
    fireEvent.submit(screen.getByRole("form", { name: /Give the medical certificate/ }));
    await wait(() => expect(calls).toContain(`POST /v1/sick-leave/${notice.id}/certificate`));
  });

  it("shows a certificate already given and whether it is verified", async () => {
    answer("GET /v1/sick-leave", { status: 200, body: { notifications: [{ ...notice, has_certificate: true, issuer: "Raffles Medical Clinic", verified: true }] } });
    render(<ReportSick roles={["employee"]} />);
    expect(await screen.findByText(/Certificate given by Raffles Medical Clinic\. Verified by the people team\./)).toBeTruthy();
    expect(screen.queryByLabelText("Who issued it")).toBeNull();
  });

  it("lets the people team verify a certificate that waits, and no one else sees the list", async () => {
    answer("GET /v1/leave-requests", { status: 200, body: { requests: [] } });
    answer("GET /v1/leave-calendar", { status: 200, body: { month: "2026-05", people: [] } });
    answer("GET /v1/sick-leave", { status: 200, body: { notifications: [{ ...notice, has_certificate: true, issuer: "Raffles Medical Clinic" }] } });
    answer(`POST /v1/sick-leave/${notice.id}/verification`, { status: 204 });
    const { unmount } = render(<TeamLeave roles={["hr_officer"]} />);
    expect(await screen.findByText("Issued by Raffles Medical Clinic")).toBeTruthy();
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByText("Verified.")).toBeTruthy();
    unmount();
    render(<TeamLeave roles={["line_manager"]} />);
    await screen.findByText("No request is waiting.");
    expect(screen.queryByText("Certificates to verify")).toBeNull();
    expect(calls.filter((c) => c === "GET /v1/sick-leave").length).toBe(3); // the people team's page asked twice (certificates, sick days beyond the limit) and reloaded once; the manager's page did not
  });

  it("is routed from the address", () => {
    expect(matchRoute("/me/leave/sick")?.route.id).toBe("report-sick");
  });
});

// req: NM-LV-05
describe("the team calendar", () => {
  const person = { person_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", name: "Ben Lim", weekdays: [1, 2, 3, 4, 5], entries: [
    { date: "2026-05-04", kind: "away", label: "Away" }, { date: "2026-05-01", kind: "holiday", label: "Labour Day" }, { date: "2026-05-05", kind: "leave", label: "Annual leave" }] };

  it("shows a letter for each day, the full words for a screen reader, and rest days apart", async () => {
    answer("GET /v1/leave-calendar", { status: 200, body: { month: "2026-05", people: [person] } });
    const { LeaveCalendar } = await import("./components/LeaveCalendar");
    const { container } = render(<LeaveCalendar start="2026-05" />);
    const away = await screen.findByLabelText("Ben Lim on 2026-05-04: Away");
    expect(away.textContent).toBe("A");
    expect(screen.getByLabelText("Ben Lim on 2026-05-01: Labour Day").textContent).toBe("H");
    expect(screen.getByLabelText("Ben Lim on 2026-05-05: Annual leave").textContent).toBe("L");
    expect(screen.getByLabelText("Ben Lim on 2026-05-02: rest day").textContent).toBe("·");
    expect(await violations(container)).toEqual([]);
  });

  it("says when nobody is in view, and moves between months", async () => {
    answer("GET /v1/leave-calendar", { status: 200, body: { month: "2026-05", people: [] } });
    const year = String(new Date().getFullYear());
    answer(`GET /v1/me/leave/${year}`, { status: 200, body: { person_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", year: Number(year), types: [] } });
    render(<MyLeave roles={["employee"]} />);
    expect(await screen.findByText("Nobody you may see is in the calendar.")).toBeTruthy();
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getByRole("button", { name: "Next month" }));
    await screen.findByText("Nobody you may see is in the calendar.");
    expect(calls.filter((c) => c === "GET /v1/leave-calendar").length).toBeGreaterThanOrEqual(2);
  });
});

// req: NM-LV-07, NM-LV-14
describe("government-paid leave and its claims", () => {
  const block = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e94", person_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", person_name: "Ben Lim", scheme: "paternity", from: "2026-06-10", to: "2026-07-07", days: 28,
    claim_deadline: "2026-10-07", guide_maximum: "10000.00", claims: [{ id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e95", amount: "6000.00", received: "2500.00", filed_on: "2026-07-20", reference: "GPL-001", state: "part_paid", over_guide: false }] };

  it("shows each block with its deadline, the guide maximum and its claims, and records a receipt", async () => {
    answer("GET /v1/government-paid-leave", { status: 200, body: { blocks: [block] } });
    answer("GET /v1/people", { status: 200, body: { people: [{ id: block.person_id, name: "Ben Lim", job_title: "Engineer" }] } });
    answer(`POST /v1/government-paid-claims/${block.claims[0]?.id}/events`, { status: 201 });
    const { container } = render(<GovernmentLeave roles={["hr_admin"]} />);
    expect(await screen.findByText("File the claim by 2026-10-07.")).toBeTruthy();
    expect(screen.getByText(/at most S\$ 10000\.00; the portal decides/)).toBeTruthy();
    expect(screen.getByText(/S\$ 6000\.00 filed on 2026-07-20, S\$ 2500\.00 received: part paid/)).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Amount received (S$)"), { target: { value: "3500.00" } });
    fireEvent.change(screen.getByLabelText("On"), { target: { value: "2026-08-20" } });
    fireEvent.submit(screen.getByRole("form", { name: /Answer to the claim filed on 2026-07-20/ }));
    expect(await screen.findByText("Saved.")).toBeTruthy();
    expect(calls).toContain(`POST /v1/government-paid-claims/${block.claims[0]?.id}/events`);
  });

  it("records a block for a child and shows the API's refusal in words", async () => {
    answer("GET /v1/government-paid-leave", { status: 200, body: { blocks: [] } });
    answer("GET /v1/people", { status: 200, body: { people: [{ id: block.person_id, name: "Ben Lim" }] } });
    answer(`GET /v1/people/${block.person_id}/dependants`, { status: 200, body: { dependants: [{ id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71", relationship: "child", birth_date: "2026-06-10", adoption_intent: false, citizenship: "citizen", effective_from: "2026-07-01" }] } });
    answer(`POST /v1/people/${block.person_id}/government-paid-leave`, { status: 422, body: { code: "NM_LEAVE_INVALID", detail: "The person had served fewer than 3 months before the child's birth." } });
    render(<GovernmentLeave roles={["hr_officer"]} />);
    await screen.findByText("No government-paid leave is recorded.");
    const { fireEvent } = await import("@testing-library/react");
    await screen.findByRole("option", { name: "Ben Lim" });
    fireEvent.change(screen.getByLabelText("Person"), { target: { value: block.person_id } });
    await screen.findByRole("option", { name: "child, born 2026-06-10" });
    fireEvent.change(screen.getByLabelText("Child"), { target: { value: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71" } });
    fireEvent.change(screen.getByLabelText("First day"), { target: { value: "2026-06-10" } });
    fireEvent.change(screen.getByLabelText("Last day"), { target: { value: "2026-07-07" } });
    fireEvent.submit(screen.getByRole("heading", { name: "Record a block of leave" }).closest("form") as HTMLFormElement);
    expect((await screen.findByRole("alert")).textContent).toMatch(/fewer than 3 months/);
  });

  it("offers Payroll no way to change anything", async () => {
    answer("GET /v1/government-paid-leave", { status: 200, body: { blocks: [block] } });
    render(<GovernmentLeave roles={["payroll_admin"]} />);
    await screen.findByText("File the claim by 2026-10-07.");
    expect(screen.queryByRole("button", { name: "Record claim" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Record leave" })).toBeNull();
  });

  it("is routed from the address", () => {
    expect(matchRoute("/people/leave/government-paid")?.route.id).toBe("government-leave");
  });
});

// req: NM-LV-10
describe("flexible work requests", () => {
  const flex = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea0", person_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", person_name: "Ben Lim", arrangement: "flexi_time", from: "2027-01-04", reason: "School run on Tuesdays.", state: "received", received_on: "2026-11-20", reply_due: "2027-01-20" };
  const leaveReq = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e80", person_id: flex.person_id, person_name: "Ben Lim", type: "annual", from: "2026-12-21", to: "2026-12-24", start_half: false, end_half: false, days: "4.00", state: "pending", requested_at: "2026-11-20T09:00:00Z" };

  it("shows own leave and flexible work with the reply deadline, sends a formal request and withdraws one", async () => {
    answer("GET /v1/leave-requests", { status: 200, body: { requests: [leaveReq] } });
    answer("GET /v1/flexible-work-requests", { status: 200, body: { requests: [flex] } });
    answer("POST /v1/me/flexible-work-requests", { status: 201, body: { id: flex.id, reply_due: "2027-01-20" } });
    answer(`POST /v1/flexible-work-requests/${flex.id}/withdrawal`, { status: 204 });
    answer(`POST /v1/leave-requests/${leaveReq.id}/cancellation`, { status: 204 });
    const { container } = render(<MyRequests roles={["employee"]} />);
    expect(await screen.findByText(/When I work \(flexi-time\) from 2027-01-04: Waiting\. The company's written reply is due by 2027-01-20\./)).toBeTruthy();
    expect(screen.getByText("2026-12-21 to 2026-12-24: 4.00 days, Waiting")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Starting on"), { target: { value: "2027-02-01" } });
    fireEvent.change(screen.getByLabelText("Why"), { target: { value: "Work from home on Fridays." } });
    fireEvent.submit(screen.getByRole("heading", { name: "Make a formal flexible work request" }).closest("form") as HTMLFormElement);
    expect(await screen.findByText("Sent. Your formal request is dated today, and the company must answer in writing by 2027-01-20.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Withdraw" }));
    expect(await screen.findByText("Withdrawn.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(calls).toContain(`POST /v1/leave-requests/${leaveReq.id}/cancellation`));
  });

  it("shows the written reply of an answered request", async () => {
    answer("GET /v1/leave-requests", { status: 200, body: { requests: [] } });
    answer("GET /v1/flexible-work-requests", { status: 200, body: { requests: [{ ...flex, state: "replied", outcome: "alternative", written_reply: "We can offer a 9am start on Tuesdays." }] } });
    render(<MyRequests roles={["employee"]} />);
    expect(await screen.findByText("The company's reply: We can offer a 9am start on Tuesdays.")).toBeTruthy();
    expect(screen.getByText(/an alternative was offered/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Withdraw" })).toBeNull();
  });

  it("lets the people team answer a request in writing, and shows the refusal for a missing reason", async () => {
    answer("GET /v1/leave-requests", { status: 200, body: { requests: [] } });
    answer("GET /v1/leave-calendar", { status: 200, body: { month: "2026-11", people: [] } });
    answer("GET /v1/sick-leave", { status: 200, body: { notifications: [] } });
    answer("GET /v1/flexible-work-requests", { status: 200, body: { requests: [flex] } });
    answer(`POST /v1/flexible-work-requests/${flex.id}/reply`, { status: 422, body: { code: "NM_LEAVE_INVALID", detail: "A refusal or an alternative gives its reason: write the business grounds, so the person can understand the decision." } });
    render(<TeamLeave roles={["hr_officer"]} />);
    expect(await screen.findByText("flexi_time from 2027-01-04: School run on Tuesdays.".replace("flexi_time", "When I work (flexi-time)"))).toBeTruthy();
    expect(screen.getByText(/Reply due: 2027-01-20/)).toBeTruthy();
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Outcome"), { target: { value: "rejected" } });
    fireEvent.click(screen.getByRole("button", { name: "Send reply" }));
    expect((await screen.findAllByRole("alert")).map((a) => a.textContent).join(" ")).toMatch(/gives its reason/);
    answer(`POST /v1/flexible-work-requests/${flex.id}/reply`, { status: 204 });
    fireEvent.change(screen.getByLabelText("Written reply to Ben Lim"), { target: { value: "The team cannot cover Tuesday afternoons during the release." } });
    fireEvent.click(screen.getByRole("button", { name: "Send reply" }));
    expect(await screen.findByText("Replied. The record of the written reply is kept.")).toBeTruthy();
  });

  it("is routed from the address", () => {
    expect(matchRoute("/me/requests")?.route.id).toBe("my-requests");
  });
});

// req: NM-TIM-04, NM-LV-08, NM-LV-09
describe("hours worked, pay inputs, unpaid sick days and the accrual", () => {
  const rec = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1eb0", person_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", person_name: "Ben Lim", date: "2026-10-08", kind: "overtime", hours: "3.00", over_limit: true, state: "pending", used_by_pay_run: false };

  it("records hours worked and says plainly when they pass the monthly limit", async () => {
    answer("GET /v1/leave-requests", { status: 200, body: { requests: [] } });
    answer("GET /v1/flexible-work-requests", { status: 200, body: { requests: [] } });
    answer("GET /v1/overtime", { status: 200, body: { records: [{ ...rec, state: "approved", over_limit: false }] } });
    answer("POST /v1/me/overtime", { status: 201, body: { id: rec.id, over_limit: true } });
    const { container } = render(<MyRequests roles={["employee"]} />);
    expect(await screen.findByText("2026-10-08: Overtime, 3.00 hours, Approved")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Day worked"), { target: { value: "2026-10-09" } });
    fireEvent.change(screen.getByLabelText("Hours worked"), { target: { value: "3" } });
    fireEvent.click(screen.getByRole("button", { name: "Record the hours" }));
    expect(await screen.findByText(/beyond the monthly limit\. It stays recorded and is paid/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Kind of work"), { target: { value: "rest_day" } });
    expect(screen.getByLabelText("Whose request was it?")).toBeTruthy();
  });

  it("lets an approver decide hours, and the people team decide sick days, read the inputs and run the accrual", async () => {
    answer("GET /v1/leave-requests", { status: 200, body: { requests: [] } });
    answer("GET /v1/leave-calendar", { status: 200, body: { month: "2026-11", people: [] } });
    answer("GET /v1/flexible-work-requests", { status: 200, body: { requests: [] } });
    answer("GET /v1/overtime", { status: 200, body: { records: [rec] } });
    answer(`POST /v1/overtime/${rec.id}/decision`, { status: 204 });
    answer("GET /v1/sick-leave", { status: 200, body: { notifications: [{ id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90", days: "2.00", paid_days: "0.00", unpaid_days: "2.00", late: false, person_id: rec.person_id, person_name: "Ben Lim", kind: "outpatient", from: "2026-03-02", to: "2026-03-03", has_certificate: false, verified: false }] } });
    answer("POST /v1/sick-leave/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90/unpaid-decision", { status: 200, body: { days: "2.00" } });
    answer("GET /v1/pay-inputs", { status: 200, body: { inputs: [{ id: "i1", person_id: rec.person_id, person_name: "Ben Lim", kind: "encashment", quantity: "2.00", unit: "days", from: "2026-11-30", to: "2026-11-30", source_type: "leave_ledger", source_id: "s1" }] } });
    answer("GET /v1/leave/accruals", { status: 200, body: { lines: [{ id: "a1", person_id: rec.person_id, person_name: "Ben Lim", state: "draft", carried_days: "4.00", annual_left: "18.00" }] } });
    answer("POST /v1/leave/accruals", { status: 201, body: { lines: 1 } });
    const { container } = render(<TeamLeave roles={["hr_admin"]} />);
    expect(await screen.findByText(/Ben Lim: 2026-10-08, Overtime, 3\.00 hours/)).toBeTruthy();
    expect(screen.getByText("Beyond the monthly limit.")).toBeTruthy();
    expect(await screen.findByText("Ben Lim: 2026-03-02 to 2026-03-03, 2.00 days beyond the limit")).toBeTruthy();
    expect(await screen.findByText("Ben Lim: encashed annual leave, 2.00 days, 2026-11-30 to 2026-11-30")).toBeTruthy();
    expect(await screen.findByText(/Ben Lim: 4\.00 days carried forward untaken, 18\.00 days of annual leave left, draft/)).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    expect(await screen.findByText("Approved.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Unpaid leave" }));
    expect(await screen.findByText("Decided.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Draft the month" }));
    expect(await screen.findByText("Drafted.")).toBeTruthy();
  });

  it("shows the people team's tiles and a dash where a figure is not theirs to see", async () => {
    answer("GET /v1/leave-requests", { status: 200, body: { requests: [{ id: "r1" }, { id: "r2" }] } });
    answer("GET /v1/checks", { status: 200, body: { findings: [{ check: 5, area: "Requests", status: "due", subject: "x", message: "m" }] } });
    render(<PeopleTiles />);
    expect(await screen.findByText("2")).toBeTruthy();
    expect(await screen.findByText("1")).toBeTruthy();
    expect(screen.getAllByText("–").length).toBeGreaterThanOrEqual(2);
    expect(screen.getByRole("link", { name: /Leave requests waiting/ }).getAttribute("href")).toBe("/people/leave");
  });
});

// req: NM-LTR-01, NM-LTR-02, NM-LTR-03, NM-LTR-07
describe("letters", () => {
  const types = { types: [
    { key: "employment_confirmation", title: "Employment confirmation", statement: "Confirms that the person works for the company.", enabled: true, due_days: 3, published_version: 1, fields: ["person.legal_name", "company.name"] },
    { key: "salary_certificate", title: "Salary certificate", statement: "States the salary.", enabled: false, due_days: 3, published_version: 0, needs: "salary", fields: [] }],
    fields: [{ key: "person.legal_name", label: "Legal name", format: "text" }, { key: "company.name", label: "Company name", format: "text" }] };
  const base = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0", person_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", person_name: "Ben Lim", type: "employment_confirmation", purpose: "A bank loan.", due_on: "2026-11-25", requested_at: "2026-11-20T10:00:00Z" };

  it("lets a person ask for a letter the company issues, and says when it is due", async () => {
    answer("GET /v1/letters/types", { status: 200, body: types });
    answer("POST /v1/me/letters", { status: 201, body: { id: base.id, due_on: "2026-11-25" } });
    const { container } = render(<RequestLetter roles={["employee"]} />);
    expect(await screen.findByText("Confirms that the person works for the company.")).toBeTruthy();
    expect(screen.queryByRole("option", { name: "Salary certificate" })).toBeNull();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("What is it for?"), { target: { value: "A bank loan." } });
    fireEvent.submit(screen.getByRole("form", { name: "Ask for a letter" }));
    expect(await screen.findByText("Sent. It is due by 2026-11-25.")).toBeTruthy();
  });

  it("lets the people team check a request, and the signatory sign only after the passkey is confirmed", async () => {
    const sha = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08";
    answer("GET /v1/letters", { status: 200, body: { requests: [{ ...base, state: "requested" }, { ...base, id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef1", state: "checked", reference: "EC-2026-0001", sha256: sha }] } });
    answer(`POST /v1/letters/${base.id}/check`, { status: 200, body: { reference: "EC-2026-0001", sha256: sha } });
    answer("POST /v1/letters/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef1/signature", { status: 200, body: { issued_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f00" } });
    const { container } = render(<Letters roles={["owner"]} />);
    expect((await screen.findAllByText(/Ben Lim: employment_confirmation, for A bank loan\.\. Due 2026-11-25\./)).length).toBe(2);
    expect(screen.getByText(`The signatory confirms this hash of the exact PDF: ${sha}`)).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getAllByRole("button", { name: "Check" })[0] as HTMLElement);
    expect(await screen.findByText("Checked. The letter is EC-2026-0001.")).toBeTruthy();
    // The passkey is not confirmed: nothing is sent.
    stepUp.result = "";
    fireEvent.click(screen.getByRole("button", { name: "Sign with my passkey" }));
    expect(await screen.findByText("The passkey was not confirmed, so nothing was signed.")).toBeTruthy();
    expect(calls).not.toContain("POST /v1/letters/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef1/signature");
    stepUp.result = undefined;
    fireEvent.click(screen.getByRole("button", { name: "Sign with my passkey" }));
    expect(await screen.findByText("Signed and issued.")).toBeTruthy();
    expect(calls).toContain("POST /v1/letters/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef1/signature");
  });

  it("offers an HR officer no signature, and a person only their own letter's download and link", async () => {
    answer("GET /v1/letters", { status: 200, body: { requests: [{ ...base, state: "checked", reference: "EC-2026-0001", sha256: "ab" }] } });
    render(<Letters roles={["hr_officer"]} />);
    await screen.findByText("Read the letter");
    expect(screen.queryByRole("button", { name: "Sign with my passkey" })).toBeNull();
    cleanup();
    answer("GET /v1/letters", { status: 200, body: { requests: [{ ...base, state: "issued", reference: "EC-2026-0001", issued_id: "x" }] } });
    const { LetterList } = await import("./components/LetterList");
    render(<LetterList mine roles={["employee"]} />);
    expect(await screen.findByRole("button", { name: "Download" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Make a link that works for seven days" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Check" })).toBeNull();
  });

  it("sets the letterhead, switches a type on, saves a draft and publishes it, and shows the API's problems in words", async () => {
    answer("GET /v1/letters/types", { status: 200, body: types });
    answer("GET /v1/letters/settings", { status: 200, body: { set: true, settings: { company_name: "Acme", uen: "201912345A", address: "1 Raffles Place" } } });
    answer("PUT /v1/letters/settings", { status: 204 });
    answer("PUT /v1/letters/types/employment_confirmation", { status: 204 });
    answer("GET /v1/letters/types/employment_confirmation/templates", { status: 200, body: { versions: [{ id: "d1", type: "employment_confirmation", version: 0, state: "draft", note: "Adds the department.", blocks: [{ kind: "paragraph", text: "Hello {{person.legal_name}}" }] }, { id: "v1", type: "employment_confirmation", version: 1, state: "published", blocks: [{ kind: "paragraph", text: "Hi" }] }] } });
    answer("PUT /v1/letters/types/employment_confirmation/template", { status: 422, body: { code: "NM_LETTER_INVALID", detail: "A template ends with a signature block: add one." } });
    answer("POST /v1/letters/templates/d1/publication", { status: 200, body: { version: 2 } });
    const { container } = render(<LetterTemplates roles={["hr_admin"]} />);
    expect(await screen.findByText("Needs the person's salary, which Nemura does not hold yet.")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getByRole("button", { name: "Save the letterhead" }));
    expect(await screen.findByText("Saved.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Choose a letter type"), { target: { value: "employment_confirmation" } });
    expect(await screen.findByText("Merge fields you may use: {{person.legal_name}}, {{company.name}}")).toBeTruthy();
    expect(await screen.findByDisplayValue("Hello {{person.legal_name}}")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Save the draft" }));
    expect((await screen.findAllByRole("alert")).map((a) => a.textContent).join(" ")).toMatch(/ends with a signature block/);
    fireEvent.click(screen.getByRole("button", { name: "Approve and publish the draft" }));
    expect(await screen.findByText("Published as version 2.")).toBeTruthy();
  });

  it("shows an HR officer the types and no way to switch them or approve a draft", async () => {
    answer("GET /v1/letters/types", { status: 200, body: types });
    render(<LetterTemplates roles={["hr_officer"]} />);
    await screen.findByText("Needs the person's salary, which Nemura does not hold yet.");
    expect(screen.queryByRole("button", { name: "Save the letterhead" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("is routed from the address", () => {
    expect(matchRoute("/me/letters/new")?.route.id).toBe("request-letter");
    expect(matchRoute("/people/letters")?.route.id).toBe("letters");
    expect(matchRoute("/settings/letters")?.route.id).toBe("letter-templates");
  });
});

// req: NM-LTR-06
describe("key employment terms", () => {
  const person = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61";
  const terms = { recorded: true, terms: { effective_from: "2026-09-01", base: { main_duties: "Keeps the books.", notice_period: "One month" }, pay: { salary_period: "monthly", basic_salary: "4500.00" } } };

  it("shows the people team the base terms and asks for the KETs letter, with no pay form", async () => {
    answer(`GET /v1/people/${person}/terms`, { status: 200, body: { recorded: true, terms: { ...terms.terms, pay: undefined } } });
    answer(`POST /v1/people/${person}/letters`, { status: 201, body: { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0", due_on: "2026-12-04" } });
    const { container } = render(<TermsPanel person={person} roles={["hr_officer"]} />);
    expect(await screen.findByText("Keeps the books.")).toBeTruthy();
    expect(screen.queryByText(/4500/)).toBeNull();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getByRole("button", { name: "Record the terms" }));
    expect(screen.getByLabelText("Notice period")).toBeTruthy();
    expect(screen.queryByLabelText("Basic salary per period (dollars)")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Ask for the KETs letter" }));
    expect(await screen.findByText("The KETs letter is requested. It is due on 2026-12-04.")).toBeTruthy();
    expect(bodies.get(`POST /v1/people/${person}/letters`)).toMatchObject({ type: "kets" });
  });

  it("shows payroll the pay terms and sends only the pay part", async () => {
    answer(`GET /v1/people/${person}/terms`, { status: 200, body: terms });
    answer(`POST /v1/people/${person}/terms`, { status: 201, body: {} });
    render(<TermsPanel person={person} roles={["payroll_admin"]} />);
    expect(await screen.findByText("4500.00 / monthly")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Ask for the KETs letter" })).toBeNull();
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getByRole("button", { name: "Record the terms" }));
    expect(screen.queryByLabelText("Notice period")).toBeNull();
    fireEvent.change(screen.getByLabelText("Salary period"), { target: { value: "monthly" } });
    fireEvent.change(screen.getByLabelText("Basic salary per period (dollars)"), { target: { value: "4800.00" } });
    fireEvent.submit(screen.getByRole("form", { name: "Record the terms" }));
    expect(await screen.findByText("The terms are saved. The earlier ones end the day before.")).toBeTruthy();
    const sent = bodies.get(`POST /v1/people/${person}/terms`) as { base?: unknown; pay?: { basic_salary: string } };
    expect(sent.base).toBeUndefined();
    expect(sent.pay?.basic_salary).toBe("4800.00");
  });

  it("draws nothing for a person who is not the people team or payroll", () => {
    const { container } = render(<TermsPanel person={person} roles={["employee"]} />);
    expect(container.textContent).toBe("");
  });
});

// req: NM-ESS-03, NM-ESS-04
describe("self-service", () => {
  const rid = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0";

  it("lets a person change their contact details and emergency contacts and ask for a change that needs evidence", async () => {
    answer("GET /v1/me/details", { status: 200, body: { phone: "+65 9123 4567", emergency_contacts: [{ name: "Mei Lim", relationship: "spouse", phone: "9123 4567" }] } });
    answer("PUT /v1/me/details", { status: 204, body: undefined });
    answer("PUT /v1/me/emergency-contacts", { status: 204, body: undefined });
    answer("POST /v1/me/details-changes", { status: 201, body: { id: rid, due_on: "2026-11-27" } });
    const { container } = render(<MyDetails roles={["employee"]} />);
    expect(await screen.findByDisplayValue("Mei Lim")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Personal email"), { target: { value: "ben@example.com" } });
    fireEvent.submit(screen.getByRole("form", { name: "Contact details" }));
    expect(await screen.findByText("Saved.")).toBeTruthy();
    expect(bodies.get("PUT /v1/me/details")).toMatchObject({ personal_email: "ben@example.com", phone: "+65 9123 4567" });
    fireEvent.click(screen.getByRole("button", { name: "Add an emergency contact" }));
    expect(screen.getByLabelText("Name of emergency contact 2")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Remove emergency contact 2" }));
    fireEvent.submit(screen.getByRole("form", { name: "Emergency contacts" }));
    expect(await screen.findByText("Your emergency contacts are saved.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("What changes"), { target: { value: "nric_fin" } });
    expect(screen.getByText(/NRIC or FIN number is not typed here/)).toBeTruthy();
    expect(screen.queryByLabelText("New legal name, as on your identity document")).toBeNull();
    fireEvent.change(screen.getByLabelText("What changed and why"), { target: { value: "My FIN became an NRIC." } });
    fireEvent.change(screen.getByLabelText("Evidence"), { target: { files: [new File(["%PDF"], "nric.pdf", { type: "application/pdf" })] } });
    fireEvent.submit(screen.getByRole("form", { name: "Ask to change my legal name, residency or NRIC or FIN" }));
    expect(await screen.findByText("Sent. The people team decides it by 2026-11-27.")).toBeTruthy();
    expect(bodies.get("POST /v1/me/details-changes")).toMatchObject({ kind: "nric_fin", evidence: { title: "nric.pdf", content_type: "application/pdf" } });
  });

  it("lets a person ask the people team a question", async () => {
    answer("POST /v1/me/questions", { status: 201, body: { id: rid, due_on: "2026-11-25" } });
    const { container } = render(<AskQuestion roles={["employee"]} />);
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Your question"), { target: { value: "When is my review?" } });
    fireEvent.submit(screen.getByRole("form", { name: "Ask a question" }));
    expect(await screen.findByText("Sent. The people team answers by 2026-11-25.")).toBeTruthy();
  });

  it("shows every request with its step and reply, and withdraws one nobody has answered", async () => {
    const row = { id: rid, kind: "question", title: "question", state: "open", step: "received", asked_at: "2026-11-20T10:00:00Z", due_on: "2026-11-25" };
    answer("GET /v1/me/requests", { status: 200, body: { requests: [row, { ...row, id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef1", kind: "details_change", title: "legal_name", state: "refused", step: "closed", reply: "Not legible." },
      { ...row, id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef2", kind: "letter", title: "employment_confirmation", state: "issued", step: "ready" }] } });
    answer(`POST /v1/me/requests/${rid}/withdrawal`, { status: 204, body: undefined });
    const { container } = render(<RequestSteps />);
    expect(await screen.findByText("Question to the people team")).toBeTruthy();
    expect(screen.getByText("Change of details: Legal name")).toBeTruthy();
    expect(screen.getByText("Reply: Not legible.")).toBeTruthy();
    expect(screen.getByText("Ready")).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "Withdraw" })).toHaveLength(1);
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getByRole("button", { name: "Withdraw" }));
    expect(await screen.findByText("Withdrawn.")).toBeTruthy();
    expect(calls).toContain(`POST /v1/me/requests/${rid}/withdrawal`);
  });

  it("lets the people team read the evidence, approve or refuse a change and answer a question", async () => {
    const base = { kind: "details_change", state: "requested", step: "received", asked_at: "2026-11-20T10:00:00Z", due_on: "2026-11-27", person_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", person: "Ben Lim" };
    answer("GET /v1/requests/queue", { status: 200, body: { queue: [{ ...base, id: rid, title: "legal_name", change_kind: "legal_name", evidence_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef9", detail: "Deed poll." },
      { ...base, id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef1", kind: "question", state: "open", title: "question", detail: "When is my review?" }] } });
    answer(`POST /v1/details-changes/${rid}/decision`, { status: 204, body: undefined });
    answer("POST /v1/questions/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef1/reply", { status: 204, body: undefined });
    const { container } = render(<PeopleRequests roles={["hr_officer"]} />);
    expect(await screen.findByText("Deed poll.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Read the evidence" }).getAttribute("href")).toBe("/api/v1/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61/documents/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef9");
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Reason for refusing Ben Lim's request"), { target: { value: "Not legible." } });
    fireEvent.click(screen.getByRole("button", { name: "Refuse Ben Lim's change" }));
    expect(await screen.findByText("Refused. The person sees the reason.")).toBeTruthy();
    expect(bodies.get(`POST /v1/details-changes/${rid}/decision`)).toMatchObject({ approve: false, reason: "Not legible." });
    fireEvent.change(screen.getByLabelText("Answer to Ben Lim"), { target: { value: "Next month." } });
    fireEvent.click(screen.getByRole("button", { name: "Send the answer to Ben Lim" }));
    expect(await screen.findByText("Answered.")).toBeTruthy();
  });
});

// req: NM-ESS-01
describe("my home", () => {
  it("shows the actions, the annual leave left, the next public holiday and the open requests, with no wrong zero for what is not given", async () => {
    const pid = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61";
    const year = new Date().getFullYear();
    answer(`GET /v1/me/leave/${year}`, { status: 200, body: { person_id: pid, year, types: [{ key: "annual", name: "Annual leave", counting: "working_days", balance: { entitled: "14.00", taken: "3.00", booked: "0.00", expired: "0.00", encashed: "0.00", left: "11.00" }, basis: [], sources: [] }] } });
    const next = new Date(year + 20, 0, 1).toISOString().slice(0, 10);
    answer("GET /v1/leave-calendar", { status: 200, body: { month: next.slice(0, 7), people: [{ person_id: pid, name: "Ben", weekdays: [1, 2, 3, 4, 5], entries: [{ date: next, kind: "holiday", label: "New Year's Day" }] }] } });
    answer("GET /v1/me/requests", { status: 200, body: { requests: [{ id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0", kind: "question", title: "question", state: "open", step: "received", asked_at: "2026-11-20T10:00:00Z", due_on: "2026-11-25" }] } });
    const { container } = render(<Home session={session} screen="me" />);
    expect(await screen.findByText("11.00 days")).toBeTruthy();
    expect(screen.getAllByRole("link", { name: "Apply for leave" })[1]?.getAttribute("href")).toBe("/me/leave/new");
    expect(screen.getAllByRole("link", { name: "Update my details" })[0]?.getAttribute("href")).toBe("/me/details");
    expect(await screen.findByText(/New Year's Day/)).toBeTruthy();
    expect(await screen.findByText("1")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
  });
});

// req: NM-ESS-06
describe("a leaver's home", () => {
  it("shows only the leaver's documents, letters and a way to ask for a copy, and says when the account ends", async () => {
    const pid = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61";
    answer(`GET /v1/people/${pid}/documents`, { status: 200, body: { documents: [{ id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e72", doc_type: "contract", title: "Employment contract", content_type: "application/pdf", created_at: "2026-01-05T09:00:00Z", retention_class: "employment_record", size_bytes: 10, uploaded_by: pid, viewers: ["employee"] }] } });
    answer("GET /v1/letters", { status: 200, body: { requests: [] } });
    answer("GET /v1/me/requests", { status: 200, body: { requests: [] } });
    answer("POST /v1/me/questions", { status: 201, body: { id: pid, due_on: "2027-01-05" } });
    const { container } = render(<LeaverHome session={{ ...session, roles: [], leaver: true, leaver_until: "2027-12-15", person_id: pid }} />);
    expect(await screen.findByRole("link", { name: "Employment contract" })).toBeTruthy();
    expect(screen.getByText(/until 2027-12-15/)).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Apply for leave" })).toBeNull();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Your question"), { target: { value: "Please send my KETs again." } });
    fireEvent.submit(screen.getByRole("form", { name: "Ask for a copy of a lost document" }));
    expect(await screen.findByText("Sent. The people team answers by 2027-01-05.")).toBeTruthy();
  });
});

// req: NM-LTR-04
describe("automatic issue", () => {
  const did = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f10";
  const types = [
    { key: "employment_confirmation", title: "Employment confirmation", statement: "x", enabled: true, due_days: 3, published_version: 1, fields: [] },
    { key: "salary_certificate", title: "Salary certificate", statement: "x", enabled: false, due_days: 3, published_version: 0, needs: "salary", fields: [] },
    { key: "kets", title: "Key employment terms", statement: "x", enabled: true, due_days: 14, published_version: 1, fields: [] },
  ];
  const live = { id: did, type: "employment_confirmation", who: "active_employees", fields: [], monthly_limit: 50, issued_this_month: 3, created_on: "2026-11-20", ends_on: "2027-06-30", state: "active" };

  it("lets the signatory delegate a standard letter only after a fresh passkey, never pay, and revoke it", async () => {
    answer("GET /v1/letters/delegations", { status: 200, body: { delegations: [live] } });
    answer("POST /v1/letters/delegations", { status: 201, body: { id: did } });
    answer(`POST /v1/letters/delegations/${did}/revocation`, { status: 204, body: undefined });
    const { container } = render(<Delegations types={types as never} signatory />);
    expect(await screen.findByText(/3 of 50 this month, until 2027-06-30 \(active\)/)).toBeTruthy();
    expect(screen.queryByRole("option", { name: "Salary certificate" })).toBeNull();
    expect(screen.queryByRole("option", { name: "Key employment terms" })).toBeNull();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Ends on (within twelve months)"), { target: { value: "2027-06-30" } });
    stepUp.result = "Passkey failed.";
    fireEvent.submit(screen.getByRole("form", { name: "Let a letter type be issued automatically" }));
    expect(await screen.findByText("Passkey failed.")).toBeTruthy();
    expect(calls).not.toContain("POST /v1/letters/delegations");
    stepUp.result = undefined;
    fireEvent.submit(screen.getByRole("form", { name: "Let a letter type be issued automatically" }));
    expect(await screen.findByText("Delegated. Requests for that letter are now issued at once.")).toBeTruthy();
    expect(bodies.get("POST /v1/letters/delegations")).toMatchObject({ type: "employment_confirmation", monthly_limit: 50, ends_on: "2027-06-30" });
    fireEvent.click(screen.getByRole("button", { name: "Revoke automatic issue of employment_confirmation" }));
    expect(await screen.findByText("Revoked. Letters already issued stay as they are.")).toBeTruthy();
  });

  it("shows the HR administrator the delegations and no way to make or revoke one", async () => {
    answer("GET /v1/letters/delegations", { status: 200, body: { delegations: [{ ...live, state: "replaced" }] } });
    render(<Delegations types={types as never} signatory={false} />);
    expect(await screen.findByText(/ended because the template changed/)).toBeTruthy();
    expect(screen.queryByRole("form")).toBeNull();
    expect(screen.queryByRole("button", { name: /Revoke/ })).toBeNull();
  });
});

// req: NM-ONB-02, NM-ONB-03
describe("joiners and leavers", () => {
  const person = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61";
  const task = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f20", person_id: person, person: "Eve On", start_date: "2026-12-01", state: "open", raised_at: "2026-11-20T10:00:00Z" };
  const leaving = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f21", person_id: person, person: "Chen Yu", kind: "resignation", notice_given_on: "2026-10-01", last_day: "2026-11-30", equipment_returned_on: "2026-11-30", nemura_access_narrows_on: "2026-12-01", service_letter: "none",
    leave_settlement: { year: 2026, entitled: "13.00", taken: "10.00", booked: "0.00", untaken: "3.00", basis: [] } };

  it("lists the invitation tasks and the leavers with their dates apart and their leave, and records and withdraws a leaving", async () => {
    answer("GET /v1/onboarding/tasks", { status: 200, body: { tasks: [task] } });
    answer("GET /v1/onboarding/settings", { status: 200, body: { invite_to_fyniq: true } });
    answer("GET /v1/offboardings", { status: 200, body: { leavings: [leaving] } });
    answer("GET /v1/people", { status: 200, body: { people: [{ id: person, name: "Chen Yu" }] } });
    answer(`POST /v1/onboarding/tasks/${task.id}/decision`, { status: 204, body: undefined });
    answer(`PUT /v1/people/${person}/offboarding`, { status: 204, body: undefined });
    answer(`POST /v1/people/${person}/offboarding/cancellation`, { status: 204, body: undefined });
    const { container } = render(<PeopleOnboarding roles={["hr_admin"]} />);
    expect(await screen.findByText("Invite Eve On, who starts 2026-12-01, to Fyniq as an employee")).toBeTruthy();
    expect(await screen.findByText("Chen Yu: Resignation, last day 2026-11-30")).toBeTruthy();
    expect(screen.getByText(/Notice given 2026-10-01; employees told of a retrenchment –/)).toBeTruthy();
    expect(screen.getByText(/entitled 13.00 days, taken 10.00, untaken 3.00/)).toBeTruthy();
    expect(screen.getByText(/becomes the narrow one for their documents on 2026-12-01/)).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getByRole("button", { name: "Mark Eve On invited" }));
    expect(await screen.findByText("Done.")).toBeTruthy();
    expect(bodies.get(`POST /v1/onboarding/tasks/${task.id}/decision`)).toMatchObject({ state: "done" });
    expect(screen.queryByLabelText("Employees told of the retrenchment")).toBeNull();
    fireEvent.change(screen.getByLabelText("How the employment ends"), { target: { value: "retrenchment" } });
    expect(screen.getByLabelText("Employees told of the retrenchment")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Last day of employment"), { target: { value: "2026-12-31" } });
    fireEvent.submit(screen.getByRole("form", { name: "Record a leaving" }));
    expect(await screen.findByText("Saved. The leaving date is on the person's record.")).toBeTruthy();
    expect(bodies.get(`PUT /v1/people/${person}/offboarding`)).toMatchObject({ kind: "retrenchment", last_day: "2026-12-31" });
    fireEvent.click(screen.getByRole("button", { name: "Withdraw Chen Yu's leaving" }));
    expect(await screen.findByText("Withdrawn. The leaving date on the record is cleared.")).toBeTruthy();
  });

  it("shows an HR officer the leavers and no invitation tasks", async () => {
    answer("GET /v1/offboardings", { status: 200, body: { leavings: [] } });
    answer("GET /v1/people", { status: 200, body: { people: [] } });
    render(<PeopleOnboarding roles={["hr_officer"]} />);
    expect(await screen.findByText("No one is leaving.")).toBeTruthy();
    expect(screen.queryByText("Invite new people to Fyniq")).toBeNull();
  });
});

// req: FR-EMP-01, NM-PAY-02, NM-PAY-03, FR-PAY-13
describe("payroll records", () => {
  const person = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61";
  const gid = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f30";

  it("is refused in words to anyone but the Payroll administrator and the Owner, and draws no pay form", () => {
    render(<PayrollHR roles={["hr_admin"]} />);
    expect(screen.getByRole("alert").textContent).toContain("Only the Payroll administrator and the Owner");
    expect(screen.queryByRole("form", { name: "Start a pay record" })).toBeNull();
    expect(screen.queryByRole("form", { name: "Make a pay group" })).toBeNull();
    expect(calls.filter((c) => c.includes("/pay"))).toEqual([]);
  });

  it("makes a pay group, starts a pay record with an allowance and a fixed deduction, imports the year to date and adds a one-off item", async () => {
    answer("GET /v1/payroll/groups", { status: 200, body: { groups: [{ id: gid, name: "Monthly", pay_day: 25, cutoff_days: 7, approval_days: 3 }] } });
    answer("GET /v1/people", { status: 200, body: { people: [{ id: person, name: "Ben Lim" }] } });
    answer(`GET /v1/people/${person}/pay`, { status: 200, body: { recorded: true, pay: { effective_from: "2026-01-05", pay_group_id: gid, pay_group: "Monthly", basic_monthly: "4500.00", shg_opt_out: false, joint_election: false, items: [] } } });
    answer(`GET /v1/people/${person}/documents`, { status: 200, body: { documents: [] } });
    answer("GET /v1/payroll/one-offs", { status: 200, body: { items: [] } });
    answer("POST /v1/payroll/groups", { status: 201, body: { id: gid } });
    answer(`POST /v1/people/${person}/pay`, { status: 201, body: undefined });
    answer(`POST /v1/people/${person}/pay/year-to-date`, { status: 201, body: undefined });
    answer(`POST /v1/people/${person}/pay/one-offs`, { status: 201, body: { id: gid } });
    const { container } = render(<PayrollHR roles={["payroll_admin"]} />);
    expect(await screen.findByText(/Monthly: paid on 25; inputs close 7 days before; approval 3 days before/)).toBeTruthy();
    expect(await screen.findByText("4500.00")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Weekly" } });
    fireEvent.submit(screen.getByRole("form", { name: "Make a pay group" }));
    expect(await screen.findByText("The pay group is made.")).toBeTruthy();
    expect(bodies.get("POST /v1/payroll/groups")).toMatchObject({ name: "Weekly", pay_day: 25, cutoff_days: 7, approval_days: 3 });
    fireEvent.click(screen.getByRole("button", { name: "Add an allowance or a fixed deduction" }));
    fireEvent.change(screen.getByLabelText("Key of item 1"), { target: { value: "transport" } });
    fireEvent.change(screen.getByLabelText("Name of item 1"), { target: { value: "Transport allowance" } });
    fireEvent.change(screen.getByLabelText("Amount of item 1"), { target: { value: "100.00" } });
    fireEvent.change(screen.getByLabelText("Basic monthly salary (dollars)"), { target: { value: "5000.00" } });
    fireEvent.submit(screen.getByRole("form", { name: "Start a pay record" }));
    expect(await screen.findByText("The pay record is saved. The one before ends the day before.")).toBeTruthy();
    expect(bodies.get(`POST /v1/people/${person}/pay`)).toMatchObject({ pay: { pay_group_id: gid, basic_monthly: "5000.00", items: [{ key: "transport", kind: "allowance", amount: "100.00", cpf_wages: true }] } });
    fireEvent.change(screen.getByLabelText("Ordinary wages subject to CPF"), { target: { value: "25000.00" } });
    fireEvent.change(screen.getByLabelText("Additional wages subject to CPF"), { target: { value: "0.00" } });
    fireEvent.change(screen.getByLabelText("Additional wages paid"), { target: { value: "0.00" } });
    fireEvent.change(screen.getByLabelText("First day of the last month counted"), { target: { value: "2026-05-01" } });
    fireEvent.submit(screen.getByRole("form", { name: "Import the year to date" }));
    expect(await screen.findByText("The year to date is imported.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Key"), { target: { value: "bonus-2026" } });
    fireEvent.change(screen.getByLabelText("Name on the payslip"), { target: { value: "Annual bonus" } });
    fireEvent.change(screen.getByLabelText("Amount (dollars)"), { target: { value: "5000.00" } });
    fireEvent.submit(screen.getByRole("form", { name: "Add a one-off item" }));
    expect(await screen.findByText("The item is added.")).toBeTruthy();
    expect(bodies.get(`POST /v1/people/${person}/pay/one-offs`)).toMatchObject({ item: { key: "bonus-2026", nature: "bonus", amount: "5000.00", cpf_wages: true } });
  });
});

// req: FR-PAY-01, FR-PAY-06, NM-PAY-09
describe("pay runs", () => {
  const person = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61";
  const gid = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f30";
  const rid = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43";
  const version = { id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f41", version: 1, state: "calculated", digest: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", prepared_by: person, prepared_at: "2026-11-20T10:00:00Z", people: 1, flags_failing: 0, flags_warning: 1,
    pack_versions: ["sg-cpf@2026.2"], totals: { gross: "5000.00", cpf_employee: "1000.00", cpf_employer: "850.00", sdl_payable: "11.00", shg: "0.00", deductions: "0.00", reimbursements: "0.00", net: "4000.00", disburse: "4000.00", employer_cost: "5861.00" } };
  const run = { id: rid, month: "2026-06-01", pay_group_id: gid, pay_group: "Monthly", pay_day: "2026-06-25", latest: version };

  it("calculates a run, shows each person's month with what to look at, overrides a figure with a reason and calculates the next version", async () => {
    answer("GET /v1/payroll/runs", { status: 200, body: { runs: [] } });
    answer("POST /v1/payroll/runs", { status: 201, body: run });
    answer(`GET /v1/payroll/runs/${rid}`, { status: 200, body: { ...run, versions: [version], overrides: [], lines: [{ person_id: person, person: "Ben Lim", gross: "5000.00", ow_subject: "5000.00", aw_subject: "0.00", cpf_total: "1850.00", cpf_employee: "1000.00", cpf_employer: "850.00", sdl: "11.25", shg: "0.00", deductions: "0.00", reimbursements: "0.00",
      held: "0.00", net: "4000.00", disburse: "4000.00", employer_cost: "5861.25", items: [], working: [], flags: [{ code: "WAGE_CLASS", fail: false, message: "Commission: the facts decide." }] }] } });
    answer(`POST /v1/payroll/runs/${rid}/overrides`, { status: 201, body: { id: gid } });
    answer(`POST /v1/payroll/runs/${rid}/versions`, { status: 201, body: { ...run, latest: { ...version, version: 2 } } });
    const { container } = render(<PayRuns month="2026-06-01" groups={[{ id: gid, name: "Monthly", pay_day: 25, cutoff_days: 7, approval_days: 3 }]} people={[{ id: person, name: "Ben Lim" }]} />);
    expect(await screen.findByText("No run for this month yet.")).toBeTruthy();
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.submit(screen.getByRole("form", { name: "Calculate a run" }));
    expect(await screen.findByText("The run is calculated: version 1.")).toBeTruthy();
    expect(bodies.get("POST /v1/payroll/runs")).toMatchObject({ month: "2026-06-01", pay_group_id: gid });
    expect(await screen.findByText(/Version 1, calculated\. Digest 9f86d081884c\. 0 failing and 1 to look at\./)).toBeTruthy();
    expect(screen.getByText("Look at: Commission: the facts decide.")).toBeTruthy();
    expect(screen.getByText(/Gross 5000.00; employee CPF 1000.00; employer CPF 850.00; SDL payable 11.00; net 4000.00; employer cost 5861.00/)).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    fireEvent.change(screen.getByLabelText("The figure to put in place (dollars)"), { target: { value: "999.00" } });
    fireEvent.change(screen.getByLabelText("Why the calculated figure is wrong"), { target: { value: "The CPF Board's own figure." } });
    fireEvent.submit(screen.getByRole("form", { name: "Override a figure" }));
    expect(await screen.findByText("The override is added. Calculate the next version to apply it.")).toBeTruthy();
    expect(bodies.get(`POST /v1/payroll/runs/${rid}/overrides`)).toMatchObject({ person_id: person, field: "cpf_employee", value: "999.00" });
    fireEvent.click(screen.getByRole("button", { name: "Calculate the next version" }));
    expect(await screen.findByText("The next version is calculated; the one before is superseded.")).toBeTruthy();
  });
});

// req: FR-PAY-07, NM-ROLE-04, NM-PAY-11
describe("approving a pay run", () => {
  const rid = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43";
  const preparer = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45";
  const other = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e46";
  const digest = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08";
  const totals = { gross: "10000.00", cpf_employee: "2000.00", cpf_employer: "1700.00", sdl_payable: "22.00", shg: "0.00", deductions: "0.00", reimbursements: "0.00", net: "8000.00", disburse: "8000.00", employer_cost: "11722.00" };
  const version = (n: number, failing: number) => ({ id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f41", version: n, state: "calculated", digest, prepared_by: preparer, prepared_at: "2026-11-20T10:00:00Z", people: 2, flags_failing: failing, flags_warning: 1, pack_versions: [], totals });
  const run = (failing: number) => ({ id: rid, month: "2026-06-01", pay_group_id: "g", pay_group: "Monthly", pay_day: "2026-06-25", latest: version(2, failing), versions: [version(2, failing)], overrides: [{ id: "o", person_id: "p", person: "Ben Lim", field: "cpf_employee", value: "999.00", reason: "The CPF Board's own figure." }],
    lines: [{ person_id: "p", person: "Ben Lim", gross: "5000.00", ow_subject: "5000.00", aw_subject: "0.00", cpf_total: "1850.00", cpf_employee: "999.00", cpf_employer: "850.00", sdl: "11.25", shg: "0.00", deductions: "0.00", reimbursements: "0.00", held: "0.00", net: "4001.00", disburse: "4001.00", employer_cost: "5861.25",
      items: [{ key: "basic", name: "Basic salary", class: "ordinary", amount: "5000.00" }], working: [{ rule: "RND-CPF", text: "Rates employer 0.17 and employee 0.20." }], flags: [{ code: "OVERRIDE", fail: false, message: "cpf_employee is overridden." }] }] });
  const changes = { has_previous: true, gross_now: "10000.00", gross_previous: "5000.00", net_now: "8000.00", net_previous: "4000.00", employer_cost_now: "11722.00", employer_cost_previous: "5861.00",
    changes: [{ person_id: "p", person: "Ben Lim", cause: "one_off", item: "bonus-2026", amount: "5000.00" }], unexplained: [{ person_id: "q", person: "Eve Ong", why: "CPF changed with no change in wages." }] };

  it("shows the digest, the flags, the override, what changed and the unexplained, and approves with a passkey over the digest", async () => {
    answer(`GET /v1/payroll/runs/${rid}`, { status: 200, body: run(0) });
    answer(`GET /v1/payroll/runs/${rid}/changes`, { status: 200, body: changes });
    answer(`POST /v1/payroll/runs/${rid}/approval`, { status: 204, body: undefined });
    stepUp.result = undefined;
    const { container } = render(<PayRunApproval id={rid} roles={["owner"]} member={other} />);
    expect(await screen.findByText(`Digest of this version, which your passkey confirms: ${digest}`)).toBeTruthy();
    expect(screen.getByText("Look at: Ben Lim: cpf_employee is overridden.")).toBeTruthy();
    expect(screen.getByText(/Ben Lim: cpf_employee set to 999.00. Reason: The CPF Board's own figure./)).toBeTruthy();
    expect(await screen.findByText("Ben Lim: one-off item 5000.00 (bonus-2026)")).toBeTruthy();
    expect(screen.getByText("Eve Ong: CPF changed with no change in wages.")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getByRole("button", { name: "Confirm with my passkey and approve version 2" }));
    expect(await screen.findByText("Approved. The inputs it used are marked used by this run.")).toBeTruthy();
    expect(bodies.get(`POST /v1/payroll/runs/${rid}/approval`)).toMatchObject({ version: 2, digest });
  });

  it("says that the preparer cannot approve and draws no button; a version with a failing check cannot be approved either", async () => {
    answer(`GET /v1/payroll/runs/${rid}`, { status: 200, body: run(0) });
    answer(`GET /v1/payroll/runs/${rid}/changes`, { status: 200, body: { ...changes, has_previous: false } });
    const first = render(<PayRunApproval id={rid} roles={["owner"]} member={preparer} />);
    expect(await screen.findByText(/You prepared this version: another Payroll administrator or the Owner approves it\./)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Confirm with my passkey/ })).toBeNull();
    first.unmount();
    answer(`GET /v1/payroll/runs/${rid}`, { status: 200, body: run(2) });
    render(<PayRunApproval id={rid} roles={["owner"]} member={other} />);
    expect(await screen.findByText(/This version has 2 failing checks/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Confirm with my passkey/ })).toBeNull();
  });

  it("publishes the payslips of an approved version and says how many were made", async () => {
    answer(`GET /v1/payroll/runs/${rid}`, { status: 200, body: { ...run(0), latest: { ...version(2, 0), state: "approved" } } });
    answer(`GET /v1/payroll/runs/${rid}/changes`, { status: 200, body: { ...changes, has_previous: false } });
    answer(`POST /v1/payroll/runs/${rid}/payslips`, { status: 201, body: { published: 2 } });
    render(<PayRunApproval id={rid} roles={["owner"]} member={other} />);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(await screen.findByRole("button", { name: "Publish the payslips of version 2" }));
    expect(await screen.findByText("Published: 2 payslips are in each person's My pay.")).toBeTruthy();
    expect(bodies.get(`POST /v1/payroll/runs/${rid}/payslips`)).toMatchObject({ version: 2 });
  });

  it("shows each person's payment and records an outcome only with a document of the person as evidence", async () => {
    const person = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45";
    answer(`GET /v1/payroll/runs/${rid}`, { status: 200, body: { ...run(0), latest: { ...version(2, 0), state: "approved" }, versions: [{ ...version(2, 0), state: "approved" }] } });
    answer(`GET /v1/payroll/runs/${rid}/changes`, { status: 200, body: { ...changes, has_previous: false } });
    answer(`GET /v1/payroll/runs/${rid}/payments`, { status: 200, body: { version: 2, state: "approved", lines: [{ person_id: person, person: "Ben Lim", owed: "3963.21", state: "unknown", attention: true, amount_mismatch: false, events: [] }] } });
    answer(`GET /v1/people/${person}/documents`, { status: 200, body: { documents: [{ id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f60", title: "Bank statement 25 Jun" }] } });
    answer(`POST /v1/payroll/runs/${rid}/payments`, { status: 201, body: undefined });
    const { container } = render(<PayRunApproval id={rid} roles={["owner"]} member={other} />);
    expect(await screen.findByText("Ben Lim: owed 3963.21, outcome unknown")).toBeTruthy();
    expect(screen.getByText(/Look at this: rejected, returned or unknown/)).toBeTruthy();
    expect(await screen.findByRole("option", { name: "Bank statement 25 Jun" })).toBeTruthy();
    expect(await violations(container)).toEqual([]);
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.click(screen.getByRole("button", { name: "Record the outcome" }));
    expect(await screen.findByText("Recorded.")).toBeTruthy();
    expect(bodies.get(`POST /v1/payroll/runs/${rid}/payments`)).toMatchObject({ person_id: person, state: "paid", amount: "3963.21", evidence_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f60" });
  });

  it("is refused in words to anyone but the Payroll administrator and the Owner", () => {
    render(<PayRunApproval id={rid} roles={["hr_admin"]} member={other} />);
    expect(screen.getByRole("alert").textContent).toContain("Only the Payroll administrator and the Owner");
  });
});

// req: NM-PAY-04, NM-ESS-02
describe("my pay", () => {
  const sid = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f50";
  const summary = { id: sid, month: "2026-06-01", payment_date: "2026-06-25", reference: "PS-202606-0001", net_salary: "3878.00", version: 2, corrected: false };
  const view = { ...summary, sha256: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", employer: "Acme Pte. Ltd.", employee: "Ben Lim", basic_salary: "4500.00", period_start: "2026-06-01", period_end: "2026-06-30",
    allowances: [{ name: "Transport allowance", amount: "100.00" }], additional_payments: [{ name: "Monthly bonus", amount: "500.00" }], deductions: [{ name: "accommodation", amount: "200.00" }, { name: "Employee's CPF contribution", amount: "1020.00" }],
    overtime_hours: "3", overtime_pay: "106.21" };

  it("lists the person's payslips with the net salary, marks a corrected one and has no axe violation", async () => {
    answer("GET /v1/me/payslips", { status: 200, body: { payslips: [summary, { ...summary, id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f51", version: 1, corrected: true }] } });
    const { container } = render(<MyPay roles={["employee"]} />);
    expect((await screen.findAllByText("2026-06: net salary 3878.00, paid 2026-06-25")).length).toBe(2);
    expect(screen.getByText("Corrected")).toBeTruthy();
    expect(await violations(container)).toEqual([]);
  });

  it("says so when no payslip has been published", async () => {
    answer("GET /v1/me/payslips", { status: 200, body: { payslips: [] } });
    render(<MyPay roles={["employee"]} />);
    expect(await screen.findByText("No payslip has been published for you yet.")).toBeTruthy();
  });

  it("shows MOM's items in order: the employer, the employee, the date of payment, the salary period, allowances, additional payments, overtime, deductions and the net salary", async () => {
    answer(`GET /v1/me/payslips/${sid}`, { status: 200, body: view });
    const { container } = render(<Payslip id={sid} roles={["employee"]} />);
    expect(await screen.findByText("Acme Pte. Ltd.")).toBeTruthy();
    for (const label of ["Employee", "Date of payment", "Salary period", "Basic salary", "Allowance: Transport allowance", "Additional payment: Monthly bonus", "Overtime hours worked", "Overtime pay", "Deduction: Employee's CPF contribution", "Net salary paid"]) {
      expect(screen.getByText(label)).toBeTruthy();
    }
    expect(screen.queryByText("Overtime payment period")).toBeNull();
    expect(await violations(container)).toEqual([]);
  });

  it("says there is no such payslip when it is not the person's", async () => {
    answer(`GET /v1/me/payslips/${sid}`, { status: 404, body: { code: "NM_NOT_FOUND" } });
    render(<Payslip id={sid} roles={["employee"]} />);
    expect(await screen.findByText("There is no such payslip of yours.")).toBeTruthy();
  });
});
