import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, newKey } from "./api/client";
import type { components } from "./api/schema";

// A person's favourites and saved views, kept on the server so they are the same on every device (FR-UX-12): a page
// pinned, or a page with the filters, columns and sort its address carries. A view's link is the address, so sharing it
// shares no data: whoever opens it sees what their own roles let them see.

export type SavedItem = components["schemas"]["SavedItem"];

const key = ["saved-items"];

export function useSavedItems(enabled = true) {
  return useQuery({
    queryKey: key,
    enabled,
    retry: false,
    staleTime: 60_000,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/saved-items", { params: { query: { page_size: 200 } } });
      if (error) throw error;
      return data.items;
    },
  });
}

// hrefOf is where an item opens: its page and its filters.
export const hrefOf = (i: Pick<SavedItem, "path" | "query">): string => `${i.path}${i.query ?? ""}`;

// useSaveActions saves or takes away an item and reads the list again: what the list shows is what the server kept.
export function useSaveActions() {
  const queries = useQueryClient();
  const refresh = () => queries.invalidateQueries({ queryKey: key });
  return {
    async save(item: { kind: "favourite" | "view"; title: string; path: string; query?: string; id?: string }): Promise<boolean> {
      const { error } = await api.POST("/v1/saved-items", { params: { header: { "Idempotency-Key": newKey() } }, body: item });
      await refresh();
      return !error;
    },
    async takeAway(item: Pick<SavedItem, "kind" | "id">): Promise<boolean> {
      const { error } = await api.DELETE("/v1/saved-items/{kind}", { params: { path: { kind: item.kind }, query: { id: item.id }, header: { "Idempotency-Key": newKey() } } });
      await refresh();
      return !error;
    },
  };
}
