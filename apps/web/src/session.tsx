import { useQuery } from "@tanstack/react-query";
import { Outlet, useRouterState } from "@tanstack/react-router";
import { ShieldCheck, Sparkles } from "lucide-react";
import { createContext, useContext, useEffect } from "react";
import { DeviceRoot } from "./components/ui/dialog";
import { api, type Organisation, type Session } from "./api/client";
import { helpText } from "./i18n";
import { setMode, useMode } from "./mode";
import { routes, type Mode } from "./registry";
import { go } from "./route";
import { AppFrame } from "./shell/AppFrame";
import { Banners } from "./shell/Banners";
import { NotificationBell } from "./shell/NotificationBell";
import { ConfirmItsYou, StepUpProvider } from "./stepup";
import { Brand, Icon } from "./ui";

export type Shell =
  | { kind: "install" }
  | { kind: "sign-in"; to: string }
  | { kind: "down" }
  | { kind: "signed-in"; session: Session; organisation?: Organisation };

// whereTo asks the API where this browser belongs: a new installation goes to /install; a browser
// without a session to /sign-in, keeping what the server sent it with (signin_error, return_to) and
// the page it asked for; a person signed in without a Fyniq account to /sign-in's invitation variant.
export async function whereTo(): Promise<Shell> {
  const [{ data: setup }, { data: session, response }] = await Promise.all([api.GET("/v1/setup"), api.GET("/v1/session")]);
  if (setup && !setup.done) return { kind: "install" };
  if (!session) {
    if (response.status !== 401) return { kind: "down" };
    const keep = new URLSearchParams();
    const params = new URLSearchParams(window.location.search);
    for (const k of ["signin_error", "return_to"]) {
      const v = params.get(k);
      if (v) keep.set(k, v);
    }
    if (!keep.has("return_to") && window.location.pathname !== "/") keep.set("return_to", window.location.pathname + window.location.search);
    return { kind: "sign-in", to: `/sign-in${keep.size ? `?${keep}` : ""}` };
  }
  if (!session.user_id) return { kind: "sign-in", to: "/sign-in" };
  // A session with no second factor may read only itself (API-AUTHZ-07): its organisation waits.
  const { data: organisation } = session.organisation_id && session.step_up_method ? await api.GET("/v1/organisation") : { data: undefined };
  return { kind: "signed-in", session, organisation };
}

export function useShell() {
  return useQuery({ queryKey: ["shell"], queryFn: whereTo });
}

export interface SignedInContext {
  session: Session;
  organisation?: Organisation;
  mode: Mode;
}

const Ctx = createContext<SignedInContext | null>(null);

// useSignedIn is the signed-in person, their organisation and their mode, for any page inside the
// signed-in layout.
export function useSignedIn(): SignedInContext {
  const c = useContext(Ctx);
  if (!c) throw new Error("useSignedIn outside the signed-in layout");
  return c;
}

// SignedInProvider gives a page (or a test) the signed-in context without the layout.
export const SignedInProvider = Ctx.Provider;

function Bare({ children }: { children: React.ReactNode }) {
  return (
    <main className="shell fy-stack" id="main">
      <Brand />
      {children}
    </main>
  );
}

// SecondFactor is the page a session that passed no second factor sees instead of the app: it can
// only confirm it's you (SEC-01, rev 0.43).
function SecondFactor({ provider }: { provider?: string }) {
  return (
    <Bare>
      <span className="fy-list__icon fy-list__icon--brand">
        <Icon of={ShieldCheck} size={20} />
      </span>
      <h1 className="fy-h1">Confirm it's you</h1>
      <p className="fy-lead">You signed in without a second factor, so Fyniq shows nothing until you confirm it's you.</p>
      <ConfirmItsYou provider={provider} onConfirmed={() => undefined} />
    </Bare>
  );
}

// useRoute is the registry's entry for the page shown.
function useRoute() {
  const path: string = useRouterState({ select: (s) => String(s.matches.at(-1)?.fullPath ?? "/") });
  return routes.find((x) => x.path === path || `${x.path}/` === path);
}

// ExpertHint says when a page of Expert mode is reached in Basic mode, such as from search or a
// link, and offers to switch (FR-UX-26). The page still shows: the mode is presentation only.
function ExpertHint() {
  return (
    <div className="fy-banner fy-banner--info fy-banner--strip" role="note">
      <Icon of={Sparkles} size={20} />
      <div className="fy-banner__text">
        <span className="fy-small">This page is part of Expert mode. Its details and the organisation's other settings show in Expert mode.</span>
      </div>
      <button type="button" className="fy-btn fy-btn--secondary fy-btn--sm" onClick={() => setMode("expert")}>
        Switch to Expert
      </button>
    </div>
  );
}

// SignedIn is the layout of every signed-in page (p1-app-shell 2.1): it asks the API where the
// browser belongs, sends it there when that is elsewhere, and otherwise shows the page inside the
// app frame, with the "Confirm it's you" dialog at hand.
export function SignedIn() {
  const shell = useShell();
  const state: Shell | { kind: "checking" } = shell.isError ? { kind: "down" } : (shell.data ?? { kind: "checking" });
  const session = state.kind === "signed-in" ? state.session : undefined;
  const mode = useMode(session?.roles);
  const route = useRoute();
  const title = helpText(route?.help ?? "home.overview").label;
  const device = useContext(DeviceRoot);

  // The kit shows .fy-basic-only and .fy-expert-only by the device's data-mode (presentation only).
  useEffect(() => {
    const el = device?.current;
    if (el) el.dataset.mode = mode;
  }, [device, mode]);

  useEffect(() => {
    // Moving between the signed-out pages is a page load, so nothing secret survives in memory.
    if (state.kind === "install") go("/install");
    else if (state.kind === "sign-in") go(state.to);
  }, [state]);

  if (state.kind !== "signed-in") {
    return (
      <Bare>
        <h1 className="fy-h1">Fyniq</h1>
        <p className="fy-lead">Intelligence behind every number.</p>
        <div role="status" aria-live="polite" className="api">
          {state.kind === "down" ? <p>The API is not running. Start it with make dev.</p> : <p>Checking the API…</p>}
        </div>
      </Bare>
    );
  }
  if (!state.session.step_up_method) return <SecondFactor provider={state.session.provider} />;
  return (
    <Ctx.Provider value={{ session: state.session, organisation: state.organisation, mode }}>
      <StepUpProvider provider={state.session.provider}>
        <AppFrame session={state.session} title={title} mode={mode} bell={<NotificationBell />} banners={<Banners />}>
          {route?.mode === "expert" && mode === "basic" && <ExpertHint />}
          <Outlet />
        </AppFrame>
      </StepUpProvider>
    </Ctx.Provider>
  );
}
