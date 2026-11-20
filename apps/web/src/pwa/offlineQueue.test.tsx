import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { answer, calls } from "../test/api";
import { noViolations } from "../test/axe";
import { installSignOutCleanup } from "./register";
import { OfflineNotice, OfflineQueuePanel, useReceiptQueue } from "./OfflineQueue";
import { memoryStore } from "./queue";

function Harness({ store }: { store: ReturnType<typeof memoryStore> }) {
  const q = useReceiptQueue("u1", () => undefined, store);
  return (
    <>
      <button type="button" onClick={() => void q.enqueue(new File(["lunch"], "lunch.pdf", { type: "application/pdf" })).then(() => undefined)}>
        capture
      </button>
      <OfflineQueuePanel q={q} />
    </>
  );
}

const renderHarness = (store = memoryStore()) =>
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <Harness store={store} />
    </QueryClientProvider>,
  );

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

// req: FR-UX-16
describe("the offline queue on screen", () => {
  it("shows a receipt waiting with how long it keeps, uploads it with its own ID as the Idempotency-Key, and says what happened", async () => {
    const seen = answer({
      "GET /v1/organisation/settings/pwa.offline_queue_hours": { status: 200, body: { key: "pwa.offline_queue_hours", value: "48" } },
      "POST /v1/captures": { status: 201, body: { id: "c1", status: "received", file_name: "lunch.pdf", version: 1 } },
    });
    const store = memoryStore();
    renderHarness(store);
    fireEvent.click(screen.getByRole("button", { name: "capture" }));
    expect(await screen.findByText("lunch.pdf")).toBeTruthy();
    expect(screen.getByText(/Kept until/)).toBeTruthy();
    await noViolations(document.body);
    const kept = (await store.all())[0]!;
    fireEvent.click(screen.getByRole("button", { name: "Upload now" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/captures"));
    expect(seen.find((s) => s.method === "POST")?.headers.get("Idempotency-Key")).toBe(kept.id);
    expect(await screen.findByText(/1 receipt uploaded/)).toBeTruthy();
    expect(await store.all()).toEqual([]);
  });

  it("keeps nothing on a shared device and says why", async () => {
    answer({ "GET /v1/organisation/settings/pwa.offline_queue_hours": { status: 403, body: { code: "API-AUTHZ-01" } } });
    localStorage.setItem("fyniq.shared-device", "1");
    const store = memoryStore();
    renderHarness(store);
    fireEvent.click(screen.getByRole("button", { name: "capture" }));
    await waitFor(async () => expect(await store.all()).toEqual([]));
    expect((screen.getByLabelText(/shared device/) as HTMLInputElement).checked).toBe(true);
  });

  it("says a refused receipt was not accepted, with the server's words", async () => {
    answer({
      "GET /v1/organisation/settings/pwa.offline_queue_hours": { status: 200, body: { value: "72" } },
      "POST /v1/captures": { status: 403, body: { code: "API-AUTHZ-01", detail: "You may not capture documents.", fix: "Ask an administrator." } },
    });
    renderHarness();
    fireEvent.click(screen.getByRole("button", { name: "capture" }));
    fireEvent.click(await screen.findByRole("button", { name: "Upload now" }));
    expect(await screen.findByText(/lunch.pdf was not accepted: You may not capture documents. Ask an administrator./)).toBeTruthy();
  });

  it("tells the person at the top of the app when there is no signal", () => {
    vi.spyOn(navigator, "onLine", "get").mockReturnValue(false);
    render(<OfflineNotice />);
    expect(screen.getByRole("status").textContent).toMatch(/offline/i);
  });
});

// req: FR-UX-16
describe("sign-out", () => {
  it("holds the sign-out form until the device is clean", async () => {
    document.body.innerHTML = '<form method="post" action="/api/auth/signout"><button type="submit">Sign out</button></form>';
    const off = installSignOutCleanup();
    const form = document.querySelector("form")!;
    form.requestSubmit = vi.fn();
    const event = new Event("submit", { cancelable: true, bubbles: true });
    form.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(true);
    await waitFor(() => expect(form.requestSubmit).toHaveBeenCalled());
    expect(form.dataset.forgotten).toBe("1");
    off();
  });
});
