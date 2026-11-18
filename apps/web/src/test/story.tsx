import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { useState, type ReactNode } from "react";
import type { Session } from "../api/client";
import { SignedInProvider } from "../session";
import { StepUpProvider } from "../stepup";
import { ownerSession } from "./api";

// InApp renders a story as a signed-in page would, without the frame and without the API: a router for its links, the
// person and their mode, and a query cache holding data, each entry under its query key, so the story shows what the
// API would have answered (QA-UI-02).
export function InApp({ children, data = [] }: { children: ReactNode; data?: [readonly unknown[], unknown][] }) {
  const [router] = useState(() => {
    const queries = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    for (const [key, value] of data) queries.setQueryData(key, value);
    const session = ownerSession as Session;
    const root = createRootRoute({
      component: () => (
        <QueryClientProvider client={queries}>
          <SignedInProvider value={{ session, mode: "expert" }}>
            <StepUpProvider provider={session.provider}>{children}</StepUpProvider>
          </SignedInProvider>
        </QueryClientProvider>
      ),
    });
    return createRouter({ routeTree: root, history: createMemoryHistory({ initialEntries: ["/"] }) });
  });
  return <RouterProvider router={router} />;
}
