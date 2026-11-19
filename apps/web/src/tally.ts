// A tally is how many items a read found, and whether there were more than one page holds; null when the person's
// roles may not read them, so the row is left out rather than shown as nothing to do (the API decides who sees what).
export interface Tally {
  n: number;
  more: boolean;
}

type Page<T> = { data?: { items?: T[] | null; next_cursor?: string }; error?: unknown };

export async function tally<T>(read: () => Promise<Page<T>>, keep: (x: T) => boolean = () => true): Promise<Tally | null> {
  const { data, error } = await read();
  if (error || !data) return null;
  return { n: (data.items ?? []).filter(keep).length, more: !!data.next_cursor };
}
