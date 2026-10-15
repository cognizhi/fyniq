import { ArrowRight, Check, CircleCheck, CircleX, DoorClosed, LifeBuoy, ShieldCheck, UserPlus } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, type Session, type SignInProvider } from "../api/client";
import { go, signInURL } from "../route";
import { Brand, Icon, problemOf } from "../ui";
import { RecoveryCodes } from "./RecoveryCodes";

// The steps of first-run setup (FR-ADM-06; docs/design/screens/src/FirstRunSetup.html). The API
// completes setup in one call and returns the recovery codes, so the person signs in first (the
// sign-in they use becomes the Super admin's), then enters the code and the company, then keeps the
// codes. Nothing secret survives a page load: the setup code and the recovery codes live only in this
// page's memory.
const steps = [
  { id: "you", label: "You" },
  { id: "code", label: "Code" },
  { id: "company", label: "Company" },
  { id: "codes", label: "Recovery" },
  { id: "second", label: "Second admin" },
  { id: "finish", label: "Finish" },
] as const;
type Step = (typeof steps)[number]["id"];

// The base currencies offered (ENG-11; the API accepts any ISO 4217 code Fyniq knows).
const currencies = [
  ["SGD", "Singapore dollar"],
  ["USD", "US dollar"],
  ["EUR", "Euro"],
  ["GBP", "Pound sterling"],
  ["AUD", "Australian dollar"],
  ["HKD", "Hong Kong dollar"],
  ["MYR", "Malaysian ringgit"],
  ["JPY", "Japanese yen"],
] as const;

type Load =
  | { kind: "checking" }
  | { kind: "down" }
  | { kind: "closed" }
  | { kind: "open"; session: Session | null; providers: SignInProvider[]; step: Step };

type Problem = { field?: "code" | "name" | "company" | "currency"; message: string };

export function Install() {
  const [load, setLoad] = useState<Load>({ kind: "checking" });

  useEffect(() => {
    (async () => {
      const [{ data: setup }, { data: session }] = await Promise.all([api.GET("/v1/setup"), api.GET("/v1/session")]);
      if (!setup) return setLoad({ kind: "down" });
      const wantsSecond = new URLSearchParams(window.location.search).get("step") === "second-admin";
      if (setup.done) {
        // After a step-up for the invitation, the Super admin comes back to that step.
        return setLoad(wantsSecond && session?.super_admin ? { kind: "open", session, providers: [], step: "second" } : { kind: "closed" });
      }
      if (session) return setLoad({ kind: "open", session, providers: [], step: "you" });
      const { data: providers } = await api.GET("/v1/sign-in-providers");
      setLoad({ kind: "open", session: null, providers: providers?.items ?? [], step: "you" });
    })().catch(() => setLoad({ kind: "down" }));
  }, []);

  return (
    <div className="fy-bare fy-enter" style={{ gridTemplateColumns: "minmax(0, 1fr)" }}>
      <main className="fy-bare__card" style={{ maxWidth: "640px" }}>
        <div className="fy-row fy-row--between fy-row--wrap">
          <Brand />
          <span className="fy-small fy-muted fy-mono">{window.location.host}</span>
        </div>
        {load.kind === "checking" && (
          <p role="status" className="fy-secondary">
            Checking this installation…
          </p>
        )}
        {load.kind === "down" && (
          <p role="status" className="fy-secondary">
            Fyniq is not answering. Try again in a moment.
          </p>
        )}
        {load.kind === "closed" && <Closed />}
        {load.kind === "open" && <Wizard session={load.session} providers={load.providers} first={load.step} />}
      </main>
    </div>
  );
}

function Closed() {
  return (
    <>
      <div className="fy-stack fy-stack--2">
        <span className="fy-eyebrow">Installation</span>
        <h1 className="fy-h1">This installation is set up</h1>
      </div>
      <section className="fy-card fy-card--pad fy-row fy-row--top" style={{ gap: "var(--space-3)" }}>
        <span className="fy-list__icon fy-list__icon--good">
          <Icon of={CircleCheck} size={20} />
        </span>
        <div className="fy-stack fy-stack--2" style={{ gap: "2px" }}>
          <span className="fy-strong">The setup page is closed for good.</span>
          <span className="fy-small fy-secondary">
            Everything else happens once you are signed in. <a href="/sign-in">Sign in</a>
          </span>
        </div>
      </section>
    </>
  );
}

function Wizard({ session, providers, first }: { session: Session | null; providers: SignInProvider[]; first: Step }) {
  const [step, setStep] = useState<Step>(first);
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [company, setCompany] = useState("");
  const [currency, setCurrency] = useState("SGD");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<Problem | null>(null);
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const summaryRef = useRef<HTMLDivElement>(null);
  const at = steps.findIndex((s) => s.id === step);

  useEffect(() => {
    if (problem) summaryRef.current?.focus();
  }, [problem]);

  const move = (to: Step) => {
    setProblem(null);
    setStep(to);
  };

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!company.trim()) return setProblem({ field: "company", message: "Enter the company's name." });
    setBusy(true);
    const { data, error } = await api.POST("/v1/setup", {
      body: { setup_code: code.trim(), display_name: name.trim(), organisation: { name: company.trim(), base_currency: currency } },
    });
    setBusy(false);
    if (data) {
      setCode(""); // used: forget it
      setRecoveryCodes(data.recovery_codes ?? []);
      return move("codes");
    }
    const p = problemOf(error);
    if (p.code === "API-SETUP-01") {
      setStep("code");
      return setProblem({
        field: "code",
        message: "That setup code isn't right, or this installation is set up already. Copy it again from the make setup-code output or Secret Manager.",
      });
    }
    if (p.code === "API-AUTHZ-04" || p.code === "API-AUTH-01") {
      setStep("you");
      return setProblem({ message: "Your sign-in has ended. Sign in again, then enter the setup code again." });
    }
    setProblem({ message: `${p.detail ?? "Setup did not complete."} ${p.fix ?? ""} (${p.code ?? "error"})` });
  };

  return (
    <>
      <div className="fy-stack fy-stack--2">
        <span className="fy-eyebrow">New installation</span>
        <h1 className="fy-h1">Set up Fyniq</h1>
        <p className="fy-lead">
          The first person to finish becomes the Super admin of this installation and the Owner of its first organisation.
        </p>
      </div>

      <div className="fy-stack fy-stack--2 fy-only-compact">
        <div className="fy-row fy-row--between fy-small">
          <span className="fy-strong">
            Step {at + 1} of {steps.length}: {steps[at]?.label}
          </span>
        </div>
        <div className="fy-progress" role="img" aria-label={`Step ${at + 1} of ${steps.length}`}>
          <span style={{ width: `${((at + 1) / steps.length) * 100}%` }} />
        </div>
      </div>
      <ol className="fy-steps fy-from-medium" aria-label="Set-up steps">
        {steps.map((s, i) => (
          <li key={s.id} className={i < at ? "fy-step fy-step--done" : "fy-step"} aria-current={i === at ? "step" : undefined}>
            <span className="fy-step__n">{i < at ? <Icon of={Check} /> : i + 1}</span>
            {s.label}
          </li>
        ))}
      </ol>

      {problem && (
        <div className="fy-banner fy-banner--critical" role="alert" tabIndex={-1} ref={summaryRef} aria-labelledby="setup-problem">
          <Icon of={CircleX} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong" id="setup-problem">
              There is a problem
            </span>
            {problem.field ? (
              <a className="fy-small" href={`#setup-${problem.field}`}>
                {problem.message}
              </a>
            ) : (
              <span className="fy-small">{problem.message}</span>
            )}
          </div>
        </div>
      )}

      {step === "you" && (
        <section className="fy-card fy-card--pad fy-stack" aria-labelledby="setup-you-title">
          <h2 className="fy-h3" id="setup-you-title">
            Your Super admin account
          </h2>
          {session ? (
            <form
              className="fy-stack"
              noValidate
              onSubmit={(e) => {
                e.preventDefault();
                if (!name.trim()) return setProblem({ field: "name", message: "Enter your name, as other people will see it." });
                move("code");
              }}
            >
              <p className="fy-secondary">
                You are signed in{session.provider ? ` with ${session.provider}` : ""}. That sign-in becomes the Super admin's.
              </p>
              <div className="fy-field">
                <label className="fy-label" htmlFor="setup-name">
                  Your name <span className="fy-req" aria-hidden="true">*</span>
                </label>
                <input className="fy-input" id="setup-name" autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} aria-invalid={problem?.field === "name"} />
              </div>
              <div className="fy-actionbar fy-actionbar--editor">
                <button className="fy-btn fy-btn--primary" type="submit">
                  Continue
                  <Icon of={ArrowRight} />
                </button>
              </div>
            </form>
          ) : (
            <>
              <p className="fy-secondary">Sign in first: the sign-in you use becomes the Super admin's.</p>
              {providers.length === 0 ? (
                <p className="fy-secondary">Sign-in is not set up for this installation yet. Ask whoever runs it.</p>
              ) : (
                <div className="fy-stack fy-stack--2">
                  {providers.map((p) => (
                    <a key={p.id} className="fy-btn fy-btn--secondary fy-btn--block provider" href={signInURL(p.id, "/install")}>
                      Continue with {p.name}
                    </a>
                  ))}
                </div>
              )}
            </>
          )}
        </section>
      )}

      {step === "code" && (
        <section className="fy-card fy-card--pad fy-stack" aria-labelledby="setup-code-title">
          <h2 className="fy-h3" id="setup-code-title">
            Setup code
          </h2>
          <form
            className="fy-stack"
            noValidate
            onSubmit={(e) => {
              e.preventDefault();
              if (code.trim().length < 20) return setProblem({ field: "code", message: "Enter the whole setup code, as it was printed." });
              move("company");
            }}
          >
            <div className="fy-field">
              <label className="fy-label" htmlFor="setup-code">
                One-time setup code <span className="fy-req" aria-hidden="true">*</span>
              </label>
              <input
                className="fy-input fy-mono"
                id="setup-code"
                autoComplete="off"
                spellCheck={false}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                aria-invalid={problem?.field === "code"}
                aria-describedby="setup-code-help"
              />
              <span className="fy-help" id="setup-code-help">
                Printed by <span className="fy-mono">make setup-code</span> on the server; in the cloud it is in Secret Manager. Case,
                spaces and dashes do not matter.
              </span>
            </div>
            <div className="fy-actionbar fy-actionbar--editor">
              <button className="fy-btn fy-btn--ghost" type="button" onClick={() => move("you")}>
                Back
              </button>
              <button className="fy-btn fy-btn--primary" type="submit">
                Continue
                <Icon of={ArrowRight} />
              </button>
            </div>
          </form>
        </section>
      )}

      {step === "company" && (
        <section className="fy-card fy-card--pad fy-stack" aria-labelledby="setup-company-title">
          <h2 className="fy-h3" id="setup-company-title">
            Your first organisation
          </h2>
          <p className="fy-secondary">You become its Owner.</p>
          <form className="fy-stack" noValidate onSubmit={create}>
            <div className="fy-field">
              <label className="fy-label" htmlFor="setup-company">
                Company name <span className="fy-req" aria-hidden="true">*</span>
              </label>
              <input className="fy-input" id="setup-company" autoComplete="organization" value={company} onChange={(e) => setCompany(e.target.value)} aria-invalid={problem?.field === "company"} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor="setup-currency">
                Base currency
              </label>
              <select className="fy-select" id="setup-currency" value={currency} onChange={(e) => setCurrency(e.target.value)} aria-describedby="setup-currency-help">
                {currencies.map(([c, n]) => (
                  <option key={c} value={c}>
                    {c}, {n}
                  </option>
                ))}
              </select>
              <span className="fy-help" id="setup-currency-help">
                The currency the books are kept in. It cannot change later.
              </span>
            </div>
            <div className="fy-actionbar fy-actionbar--editor">
              <button className="fy-btn fy-btn--ghost" type="button" onClick={() => move("code")} disabled={busy}>
                Back
              </button>
              <button className="fy-btn fy-btn--primary" type="submit" disabled={busy}>
                {busy ? "Setting up…" : "Create organisation"}
                <Icon of={ArrowRight} />
              </button>
            </div>
          </form>
        </section>
      )}

      {step === "codes" && <RecoveryCodes codes={recoveryCodes} onDone={() => (setRecoveryCodes([]), move("second"))} doneLabel="Continue" />}

      {step === "second" && <SecondAdmin session={session} onDone={() => move("finish")} />}

      {step === "finish" && (
        <section className="fy-card fy-card--pad fy-stack" aria-labelledby="setup-finish-title">
          <h2 className="fy-h3" id="setup-finish-title">
            Finish
          </h2>
          <div className="fy-banner fy-banner--warning">
            <Icon of={DoorClosed} size={20} />
            <div className="fy-banner__text">
              <span className="fy-strong">This page is closed for good</span>
              <span className="fy-small fy-secondary">The setup code is used and this page never opens again.</span>
            </div>
          </div>
          <div className="fy-actionbar fy-actionbar--editor">
            <button className="fy-btn fy-btn--primary" type="button" onClick={() => go("/")}>
              Go to Fyniq
              <Icon of={ArrowRight} />
            </button>
          </div>
        </section>
      )}
    </>
  );
}

// SecondAdmin recommends a second Super admin (FR-ADM-06): an invitation code to give them by a channel
// you trust; they sign in and enter it. Super admin changes need a step-up in the last 5 minutes
// (FR-ADM-07): a sign-in that passed a second factor.
function SecondAdmin({ session, onDone }: { session: Session | null; onDone: () => void }) {
  const [busy, setBusy] = useState(false);
  const [invitation, setInvitation] = useState<{ code: string; expires: string } | null>(null);
  const [stepUp, setStepUp] = useState(false);
  const [failure, setFailure] = useState("");

  const invite = async () => {
    setBusy(true);
    setFailure("");
    const { data, error } = await api.POST("/v1/super-admins/invitations");
    setBusy(false);
    if (data) return setInvitation({ code: data.code, expires: new Date(data.expires_at).toLocaleDateString("en-SG", { day: "numeric", month: "short", year: "numeric" }) });
    const p = problemOf(error);
    if (p.code === "API-AUTHZ-05") return setStepUp(true);
    setFailure(`${p.detail ?? "The invitation was not created."} (${p.code ?? "error"})`);
  };

  return (
    <section className="fy-card fy-card--pad fy-stack" aria-labelledby="setup-second-title">
      <div className="fy-row fy-row--between">
        <h2 className="fy-h3" id="setup-second-title">
          Add a second Super admin
        </h2>
        <span className="fy-tag">Recommended</span>
      </div>
      <p className="fy-secondary">
        With two, nobody is locked out if one of you loses access: the other confirms your recovery. Fyniq allows two Super admins.
      </p>
      {invitation ? (
        <div className="fy-stack fy-stack--3" role="status">
          <div className="fy-field">
            <span className="fy-label">Invitation code</span>
            <output className="fy-input fy-mono" aria-label="Invitation code">
              {invitation.code}
            </output>
            <span className="fy-help">
              Shown once. Give it to them in person or on a call, never in a shared chat or a ticket. They sign in to Fyniq and enter it.
              It works once and expires on {invitation.expires}.
            </span>
          </div>
        </div>
      ) : stepUp ? (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={ShieldCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Confirm it's you first</span>
            <span className="fy-small fy-secondary">
              Inviting a Super admin needs a sign-in with your second step in the last 5 minutes. Sign in again, then you come back
              here.
            </span>
            {session?.provider && (
              <a className="fy-btn fy-btn--secondary" href={signInURL(session.provider, "/install?step=second-admin")}>
                Sign in again
              </a>
            )}
          </div>
        </div>
      ) : (
        <div className="fy-row fy-row--wrap">
          <button className="fy-btn fy-btn--secondary" type="button" onClick={invite} disabled={busy}>
            <Icon of={UserPlus} />
            {busy ? "Creating…" : "Create an invitation"}
          </button>
        </div>
      )}
      {failure && (
        <p className="fy-error" role="alert">
          <Icon of={CircleX} />
          {failure}
        </p>
      )}
      <div className="fy-banner fy-banner--info">
        <Icon of={LifeBuoy} size={20} />
        <div className="fy-banner__text">
          <span className="fy-strong">If a Super admin loses access</span>
          <span className="fy-small fy-secondary">
            One of their printed recovery codes and the other Super admin's confirmation give access back. If no Super admin can sign in,
            only the two-person recovery held by whoever runs this deployment restores it.
          </span>
        </div>
      </div>
      <div className="fy-actionbar fy-actionbar--editor">
        <button className="fy-btn fy-btn--primary" type="button" onClick={onDone}>
          {invitation ? "Continue" : "Later"}
          <Icon of={ArrowRight} />
        </button>
      </div>
    </section>
  );
}
