import { describe, expect, it } from "vitest";
import { memoryStore, Queue, QueueRefused, sha256, type Outcome } from "./queue";

const file = (name = "receipt.pdf", body = "a receipt") => new File([body], name, { type: "application/pdf" });
const HOUR = 3600_000;

// req: FR-UX-16
describe("the offline receipt queue", () => {
  it("keeps a receipt sealed, not as the file, and gives it back the same when it uploads", async () => {
    const store = memoryStore();
    let at = 1_000_000;
    const q = new Queue(store, "u1", 72, () => at, () => false);
    const m = await q.enqueue(file("lunch.pdf", "S$18.40 at the cafe"));
    expect(m.name).toBe("lunch.pdf");
    expect(m.sha256).toBe(await sha256(new TextEncoder().encode("S$18.40 at the cafe").buffer as ArrayBuffer));
    const kept = (await store.all())[0]!;
    expect(new TextDecoder().decode(kept.data)).not.toContain("cafe"); // what is stored is not the file
    expect(kept.iv.length).toBe(12);
    const sent: { text: string; id: string; hash: string }[] = [];
    const r = await q.flush(async (f, id, hash) => {
      sent.push({ text: await f.text(), id, hash });
      return { kind: "uploaded" };
    });
    expect(sent).toEqual([{ text: "S$18.40 at the cafe", id: m.id, hash: m.sha256 }]);
    expect(r).toEqual({ uploaded: 1, duplicates: 0, refused: [], left: 0 });
    expect(await store.all()).toEqual([]);
    at += 1;
  });

  it("drops a receipt when its time is up, and keeps it until then", async () => {
    let at = 0;
    const q = new Queue(memoryStore(), "u1", 72, () => at, () => false);
    await q.enqueue(file());
    at = 71 * HOUR;
    expect(await q.list()).toHaveLength(1);
    at = 72 * HOUR;
    expect(await q.list()).toHaveLength(0);
  });

  it("is off on a shared device, and refuses what is too big or too many", async () => {
    const shared = new Queue(memoryStore(), "u1", 72, Date.now, () => true);
    await expect(shared.enqueue(file())).rejects.toMatchObject({ code: "shared-device" });
    const q = new Queue(memoryStore(), "u1", 72, Date.now, () => false);
    await expect(q.enqueue(new File([new Uint8Array(20 * 1024 * 1024 + 1)], "big.pdf"))).rejects.toBeInstanceOf(QueueRefused);
    for (let i = 0; i < 50; i++) await q.enqueue(file(`r${i}.pdf`, `r${i}`));
    await expect(q.enqueue(file("one-more.pdf"))).rejects.toMatchObject({ code: "full" });
  });

  it("shows and uploads a person's own receipts only, and clears everything at sign-out", async () => {
    const store = memoryStore();
    const a = new Queue(store, "u1", 72, Date.now, () => false);
    const b = new Queue(store, "u2", 72, Date.now, () => false);
    await a.enqueue(file("a.pdf", "a"));
    await b.enqueue(file("b.pdf", "b"));
    expect((await a.list()).map((m) => m.name)).toEqual(["a.pdf"]);
    expect((await b.list()).map((m) => m.name)).toEqual(["b.pdf"]);
    const names: string[] = [];
    await a.flush(async (f) => (names.push(f.name), { kind: "uploaded" } as Outcome));
    expect(names).toEqual(["a.pdf"]);
    expect((await b.list()).length).toBe(1);
    await b.clear();
    expect(await store.all()).toEqual([]);
  });

  it("leaves what could not be sent for the next try, drops what the server refused with its reason, and counts a duplicate once", async () => {
    const q = new Queue(memoryStore(), "u1", 72, () => Date.now(), () => false);
    for (const n of ["one", "two", "three", "four"]) await q.enqueue(file(`${n}.pdf`, n));
    const answers: Outcome[] = [{ kind: "duplicate" }, { kind: "refused", reason: "You may no longer capture documents." }, { kind: "retry" }];
    const r = await q.flush(async () => answers.shift() as Outcome);
    expect(r.duplicates).toBe(1);
    expect(r.refused).toEqual([{ name: "two.pdf", reason: "You may no longer capture documents." }]);
    expect(r.uploaded).toBe(0);
    expect(r.left).toBe(2); // the third failed to send and stays, with the fourth
    const again = await q.flush(async () => ({ kind: "uploaded" }));
    expect(again).toMatchObject({ uploaded: 2, left: 0 });
  });

  it("treats a thrown upload as a retry", async () => {
    const q = new Queue(memoryStore(), "u1", 72, Date.now, () => false);
    await q.enqueue(file());
    const r = await q.flush(async () => {
      throw new Error("offline");
    });
    expect(r.left).toBe(1);
  });
});
