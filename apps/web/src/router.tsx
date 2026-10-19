import { CSPProvider } from "@base-ui/react/csp-provider";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRootRoute, createRoute, createRouter, lazyRouteComponent, Outlet, type RouteComponent } from "@tanstack/react-router";
import { useMotion } from "./appearance";
import { routes, type RouteId } from "./registry";
import { Home } from "./screens/Home";

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
};

function Root() {
  return (
    // Base UI renders no inline <style> element: the CSP allows styles from the origin only.
    <CSPProvider disableStyleElements>
      <QueryClientProvider client={queryClient}>
        <div className="fy-device" data-motion={useMotion()}>
          <Outlet />
        </div>
      </QueryClientProvider>
    </CSPProvider>
  );
}

// An unknown address shows Home, as in P0: Home sends the browser where it belongs.
const rootRoute = createRootRoute({ component: Root, notFoundComponent: Home });

const routeTree = rootRoute.addChildren(routes.map((r) => createRoute({ getParentRoute: () => rootRoute, path: r.path, component: screens[r.id] })));

export function makeRouter() {
  return createRouter({ routeTree, defaultPreload: "intent" });
}

export const router = makeRouter();

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
