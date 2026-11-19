import { useEffect, useState } from "react";
import { t } from "./i18n";
import { Home } from "./pages/Home";
import { NotFound } from "./pages/NotFound";
import { SignIn } from "./pages/SignIn";
import { navigate } from "./route";
import { routes } from "./registry";
import { useSession } from "./session";

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
  const route = routes.find((r) => r.path === path);
  const signedOut = state.status === "none";
  const landing = state.status === "in" ? state.session.landing : "/sign-in";
  useEffect(() => {
    if (state.status === "loading") return;
    if (path === "/") navigate(landing, true);
    else if (route?.signedIn && signedOut) navigate("/sign-in", true);
    else if (route?.id === "sign-in" && state.status === "in") navigate(landing, true);
  }, [path, route, state.status, landing, signedOut]);

  const body = (() => {
    if (state.status === "loading") return <p role="status" className="fy-secondary">{t("session.loading")}</p>;
    if (path === "/sign-in") return <SignIn returnTo={landing === "/sign-in" ? "/" : landing} />;
    if (!route) return <NotFound home={landing} />;
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
