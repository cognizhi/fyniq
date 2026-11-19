import { useEffect, useState } from "react";
import { t } from "./i18n";
import { NewPerson } from "./pages/NewPerson";
import { Features } from "./pages/Features";
import { Directory } from "./pages/Directory";
import { Home } from "./pages/Home";
import { Join } from "./pages/Join";
import { NotFound } from "./pages/NotFound";
import { Person } from "./pages/Person";
import { SignIn } from "./pages/SignIn";
import { navigate } from "./route";
import { matchRoute } from "./registry";
import { useSession } from "./session";

// joinKind is the kind of code the join page asks for, from its address: setup, recovery, or (anything else) an invitation.
function joinKind(): "setup" | "invitation" | "recovery" {
  const k = new URLSearchParams(window.location.search).get("kind");
  return k === "setup" || k === "recovery" ? k : "invitation";
}

// pathname follows the address bar, so the browser's back and forward buttons and `navigate` both move the app.
function usePathname(): string {
  const [path, set] = useState(window.location.pathname);
  useEffect(() => {
    const on = () => set(window.location.pathname);
    window.addEventListener("popstate", on);
    return () => window.removeEventListener("popstate", on);
  }, []);
  return path;
}

// The app's pages. A signed-out person goes to sign-in and comes back to where they were; a signed-in one never sees sign-in, and lands by role.
export function App() {
  const path = usePathname();
  const state = useSession();
  const matched = matchRoute(path);
  const route = matched?.route;
  const signedOut = state.status === "none";
  const landing = state.status === "in" ? state.session.landing : "/sign-in";
  useEffect(() => {
    if (state.status === "loading") return;
    if (path === "/") navigate(landing, true);
    else if (route?.signedIn && signedOut) navigate("/sign-in", true);
    else if ((route?.id === "sign-in" || route?.id === "join") && state.status === "in") navigate(landing, true);
  }, [path, route, state.status, landing, signedOut]);

  const body = (() => {
    if (state.status === "loading") return <p role="status" className="fy-secondary">{t("session.loading")}</p>;
    if (path === "/join") return <Join kind={joinKind()} />;
    if (path === "/sign-in") return <SignIn returnTo={landing === "/sign-in" ? "/" : landing} />;
    if (!route) return <NotFound home={landing} />;
    if (state.status === "in" && route.id === "people-directory") return <Directory roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "person-new") return <NewPerson roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "features") return <Features roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "person") return <Person id={matched?.params.id ?? ""} roles={state.session.roles ?? []} />;
    if (state.status === "in") return <Home session={state.session} screen={route.id === "people-home" ? "people" : "me"} />;
    return null;
  })();
  return (
    <>
      <a className="fy-btn fy-btn--primary nm-skip" href="#main">{t("app.skip")}</a>
      {body}
    </>
  );
}
