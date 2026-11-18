import { act, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { answer, calls } from "./test/api";
import { renderPage } from "./test/render";
import { FEED_WINDOW, installFreshness } from "./freshness";
import { Contacts } from "./screens/contacts/Contacts";

let state: DocumentVisibilityState = "visible";
Object.defineProperty(document, "visibilityState", { configurable: true, get: () => state });
const show = (s: DocumentVisibilityState) => {
  state = s;
  document.dispatchEvent(new Event("visibilitychange"));
};

afterEach(() => {
  state = "visible";
  vi.useRealTimers();
});

const minutes = (n: number) => n * 60 * 1000;

// req: G4-077
// (FR-UX-19's live feed itself is not built: this is what a page does when it comes back after missing changes.)
describe("a page back after missing what changed", () => {
  it("reloads its state once when it was hidden, offline or asleep past the feed window, and not before", () => {
    vi.useFakeTimers();
    let t = 0;
    const client = { invalidateQueries: vi.fn() };
    const stop = installFreshness(client as never, document, () => t);
    show("hidden");
    t += minutes(2);
    show("visible");
    expect(client.invalidateQueries).not.toHaveBeenCalled();
    show("hidden");
    t += FEED_WINDOW + 1;
    show("visible");
    show("hidden");
    show("visible"); // back again at once: nothing more to reload
    expect(client.invalidateQueries).toHaveBeenCalledOnce();
    window.dispatchEvent(new Event("offline"));
    t += minutes(10);
    window.dispatchEvent(new Event("online"));
    expect(client.invalidateQueries).toHaveBeenCalledTimes(2);
    t += minutes(12); // asleep: no timer fired meanwhile
    vi.advanceTimersByTime(30_000);
    expect(client.invalidateQueries).toHaveBeenCalledTimes(3);
    vi.advanceTimersByTime(30_000); // awake again: ticks on time
    expect(client.invalidateQueries).toHaveBeenCalledTimes(3);
    stop();
  });

  it("re-reads its list once: a record restricted meanwhile is gone and none shows twice", async () => {
    const contact = (id: string, name: string) => ({ id, legal_name: name, customer: true, vendor: false, active: true, version: 1 });
    answer({ "GET /v1/contacts": { status: 200, body: { items: [contact("c1", "Acme Analytics Pte Ltd"), contact("c2", "Board Secret Ltd")] } } });
    const page = renderPage(Contacts, { path: "/contacts" });
    expect(await screen.findByText("Board Secret Ltd")).toBeTruthy();
    let t = 0;
    const stop = installFreshness(page.queries, document, () => t);
    // Meanwhile the second contact was restricted: the API no longer lists it to this person.
    answer({ "GET /v1/contacts": { status: 200, body: { items: [contact("c1", "Acme Analytics Pte Ltd")] } } });
    act(() => show("hidden"));
    t += FEED_WINDOW + minutes(1);
    act(() => show("visible"));
    await waitFor(() => expect(screen.queryByText("Board Secret Ltd")).toBeNull());
    expect(screen.getAllByText("Acme Analytics Pte Ltd")).toHaveLength(1);
    expect(calls().filter((c) => c === "GET /v1/contacts")).toHaveLength(1); // one reload since the new answers
    stop();
  });
});
