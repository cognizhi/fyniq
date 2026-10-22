import { CSPProvider } from "@base-ui/react/csp-provider";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRootRoute, createRoute, createRouter, lazyRouteComponent, Outlet, type AnyRoute, type RouteComponent } from "@tanstack/react-router";
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
  inbox: lazyRouteComponent(() => import("./screens/approvals/Inbox"), "Inbox"),
  approval: lazyRouteComponent(() => import("./screens/approvals/ApprovalPage"), "ApprovalPage"),
  "review-log": lazyRouteComponent(() => import("./screens/approvals/ReviewLog"), "ReviewLog"),
  contacts: lazyRouteComponent(() => import("./screens/contacts/Contacts"), "Contacts"),
  items: lazyRouteComponent(() => import("./screens/contacts/Items"), "Items"),
  bills: lazyRouteComponent(() => import("./screens/purchases/Bills"), "Bills"),
  "bill-new": lazyRouteComponent(() => import("./screens/purchases/BillPage"), "BillPage"),
  "credit-new": lazyRouteComponent(() => import("./screens/purchases/BillPage"), "BillPage"),
  bill: lazyRouteComponent(() => import("./screens/purchases/BillPage"), "BillPage"),
  "recurring-bills": lazyRouteComponent(() => import("./screens/purchases/RecurringBills"), "RecurringBills"),
  "recurring-bill-new": lazyRouteComponent(() => import("./screens/purchases/RecurringBillPage"), "RecurringBillPage"),
  "recurring-bill": lazyRouteComponent(() => import("./screens/purchases/RecurringBillPage"), "RecurringBillPage"),
  accounting: lazyRouteComponent(() => import("./screens/accounting/Accounting"), "Accounting"),
  journals: lazyRouteComponent(() => import("./screens/accounting/Journals"), "Journals"),
  "journal-new": lazyRouteComponent(() => import("./screens/accounting/JournalPage"), "JournalPage"),
  journal: lazyRouteComponent(() => import("./screens/accounting/JournalPage"), "JournalPage"),
  "recurring-journals": lazyRouteComponent(() => import("./screens/accounting/RecurringJournals"), "RecurringJournals"),
  "recurring-journal-new": lazyRouteComponent(() => import("./screens/accounting/RecurringJournalPage"), "RecurringJournalPage"),
  "recurring-journal": lazyRouteComponent(() => import("./screens/accounting/RecurringJournalPage"), "RecurringJournalPage"),
  periods: lazyRouteComponent(() => import("./screens/accounting/Periods"), "Periods"),
  "opening-balances": lazyRouteComponent(() => import("./screens/accounting/OpeningBalances"), "OpeningBalances"),
  "period-close": lazyRouteComponent(() => import("./screens/accounting/PeriodClose"), "PeriodClose"),
  "general-ledger": lazyRouteComponent(() => import("./screens/accounting/GeneralLedger"), "GeneralLedger"),
  "chart-of-accounts": lazyRouteComponent(() => import("./screens/accounting/ChartOfAccounts"), "ChartOfAccounts"),
  currencies: lazyRouteComponent(() => import("./screens/accounting/Currencies"), "Currencies"),
  settings: lazyRouteComponent(() => import("./screens/settings/Settings"), "Settings"),
  "settings-profile": lazyRouteComponent(() => import("./screens/settings/Profile"), "Profile"),
  "settings-organisation": lazyRouteComponent(() => import("./screens/settings/Organisation"), "Organisation"),
  "settings-accounting": lazyRouteComponent(() => import("./screens/settings/Accounting"), "Accounting"),
  "settings-statutory-packs": lazyRouteComponent(() => import("./screens/settings/StatutoryPacks"), "StatutoryPacks"),
  "settings-members": lazyRouteComponent(() => import("./screens/settings/Members"), "Members"),
  "settings-team-mode": lazyRouteComponent(() => import("./screens/settings/TeamMode"), "TeamMode"),
  "settings-approval-policies": lazyRouteComponent(() => import("./screens/settings/ApprovalPolicies"), "ApprovalPolicies"),
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

// The routes come from the registry's data, so TypeScript sees one route type for all of them; each
// is typed as any route, and a link to a page with a parameter passes its params by name.
const routeTree = rootRoute.addChildren([
  signedIn.addChildren(routes.filter((r) => r.signedIn).map((r): AnyRoute => createRoute({ getParentRoute: () => signedIn, path: r.path, component: screens[r.id] }))),
  ...routes.filter((r) => !r.signedIn).map((r): AnyRoute => createRoute({ getParentRoute: () => rootRoute, path: r.path, component: screens[r.id] })),
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
