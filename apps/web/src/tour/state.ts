import { useQuery, type QueryClient } from "@tanstack/react-query";
import { useSyncExternalStore } from "react";
import { api, newKey } from "../api/client";
import type { components } from "../api/schema";

// The person's tour state on the server (FR-UX-34, G4-104): the tour's only write. Saves go one after another, each
// with the ETag the last one returned.

export type HelpState = components["schemas"]["HelpState"];

export function useHelpState(enabled = true) {
  return useQuery({
    queryKey: ["help-state"],
    enabled,
    retry: false,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/help-state");
      if (error) throw error;
      return data;
    },
  });
}

let chain: Promise<unknown> = Promise.resolve();

// saveTour saves where the person is with the tour; a refused save is dropped (the tour goes on, and the next save
// starts from the server's state).
export function saveTour(queryClient: QueryClient, tour: "started" | "finished" | "skipped", step: number): Promise<unknown> {
  chain = chain.then(async () => {
    const current = queryClient.getQueryData<HelpState>(["help-state"]);
    const { data } = await api.PUT("/v1/help-state/tour", {
      params: { header: { "If-Match": current?.etag ?? "*", "Idempotency-Key": newKey() } },
      body: { tour, step },
    });
    if (data) queryClient.setQueryData(["help-state"], data); // the server's answer (FR-UX-22)
    else await queryClient.invalidateQueries({ queryKey: ["help-state"] });
  });
  return chain;
}

// Whether the tour is running, so the offer steps aside meanwhile.
let running = false;
const runners = new Set<() => void>();

export function setTourRunning(on: boolean): void {
  running = on;
  for (const l of runners) l();
}

export function useTourRunning(): boolean {
  return useSyncExternalStore(
    (l) => {
      runners.add(l);
      return () => runners.delete(l);
    },
    () => running,
  );
}

// The tour starts from the offer, the Help menu and the command palette.
const listeners = new Set<(at: number) => void>();

export function startTour(at = 0): void {
  for (const l of listeners) l(at);
}

export function onTourStart(l: (at: number) => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}
