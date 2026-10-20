import { CSPProvider } from "@base-ui/react/csp-provider";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRootRoute, createRoute, createRouter, lazyRouteComponent, Outlet, type RouteComponent } from "@tanstack/react-router";
import { useRef } from "react";
import { useMotion } from "./appearance";
import { DeviceRoot } from "./components/ui/dialog";
import { routes, type RouteId } from "./registry";
import { Home } from "./screens/Home";
import { SignedIn } from "./session";

// The routes of the web app (ADR-T1: TanStack Router), built from the feature registry so that a
// page cannot exist without its mode tag, help key and guide page (FR-UX-29, FR-UX-31). Every
// screen reads the API through the generated client (QA-API-03); the API decides who may see what.
//
// Data comes through TanStack Query. Ledger figures are never written into its cache before the
// server answers (FR-UX-22): a mutation invalidates and refetches.
export const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
});

// Each registry route names its screen; TypeScript fails when one is missing. Pages other than Home
// load on demand and are preloaded on intent (NFR-PERF-UI-03).
const screens: Record<RouteId, RouteComponent> = {
  home: Home,
  "sign-in": lazyRouteComponent(() => import("./screens/SignIn"), "SignIn"),
  install: lazyRouteComponent(() => import("./screens/Install"), "Install"),
  settings: lazyRouteComponent(() => import("./screens/settings/Settings"), "Settings"),
  "settings-profile": lazyRouteComponent(() => import("./screens/settings/Profile"), "Profile"),
};

function Root() {
  const device = useRef<HTMLDivElement>(null);
  return (
    // Base UI renders no inline <style> element: the CSP allows styles from the origin only.
    <CSPProvider disableStyleElements>
      <QueryClientProvider client={queryClient}>
        <div className="fy-device" data-motion={useMotion()} ref={device}>
          <DeviceRoot.Provider value={device}>
            <Outlet />
          </DeviceRoot.Provider>
        </div>
      </QueryClientProvider>
    </CSPProvider>
  );
}

// An unknown address says so, with the way Home (an address sign-in sent the browser to, such as a
// page of a module not built yet, ends here too).
function NotFound() {
  return (
    <main className="shell fy-stack" id="main">
      <h1 className="fy-h1">There is no page here</h1>
      <p className="fy-lead">The address may be mistyped, or the page is not in Fyniq yet.</p>
      <a className="fy-btn fy-btn--primary" href="/">
        Go to Home
      </a>
    </main>
  );
}

const rootRoute = createRootRoute({ component: Root, notFoundComponent: NotFound });

// Signed-in pages share one layout without a path of its own: it asks the API where the browser
// belongs and wraps the page in the app frame (session.tsx). Sign-in and setup stand alone.
const signedIn = createRoute({ getParentRoute: () => rootRoute, id: "signed-in", component: SignedIn });

const routeTree = rootRoute.addChildren([
  signedIn.addChildren(routes.filter((r) => r.signedIn).map((r) => createRoute({ getParentRoute: () => signedIn, path: r.path, component: screens[r.id] }))),
  ...routes.filter((r) => !r.signedIn).map((r) => createRoute({ getParentRoute: () => rootRoute, path: r.path, component: screens[r.id] })),
]);

export function makeRouter() {
  return createRouter({ routeTree, defaultPreload: "intent" });
}

export const router = makeRouter();

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
