import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import type { ComponentType } from "react";
import type { Session } from "../api/client";
import type { Mode } from "../registry";
import { SignedInProvider } from "../session";
import { StepUpProvider } from "../stepup";
import { ownerSession } from "./api";

// renderPage renders one signed-in page as the signed-in layout would, without the frame: a fresh
// query cache, the person and their mode, the "Confirm it's you" dialog, and a router at `path`.
interface Options {
  path?: string; // the address shown
  route?: string; // the route's pattern, such as "/approvals/$approvalId"; the address by default
  session?: Session;
  mode?: Mode;
}

export function renderPage(Page: ComponentType, { path = "/", session = ownerSession as Session, mode = "expert", route }: Options = {}) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute();
  const page = createRoute({
    getParentRoute: () => root,
    path: route ?? path,
    component: () => (
      <QueryClientProvider client={queries}>
        <SignedInProvider value={{ session, mode }}>
          <StepUpProvider provider={session.provider}>
            <Page />
          </StepUpProvider>
        </SignedInProvider>
      </QueryClientProvider>
    ),
  });
  const router = createRouter({ routeTree: root.addChildren([page]), history: createMemoryHistory({ initialEntries: [path] }) });
  return { queries, ...render(<RouterProvider router={router} />) };
}
