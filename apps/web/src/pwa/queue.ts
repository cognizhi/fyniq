// The offline receipt queue (FR-UX-16, rev 0.8; p2-mobile-approvals-automation design D4). A receipt captured with no signal is kept on the device until there is one, and uploaded then.
// What the PRD asks of it, and how it is kept:
//  - encrypted on the device: each file is sealed with AES-GCM under a key made on the device and kept as a non-extractable CryptoKey, so a copy of the browser's storage holds no readable receipt;
//  - for a limited time: a receipt waits at most `hours` (the organisation's setting, 72 by default) and is dropped when its time is up;
//  - removed at sign-out, and never kept on a device the person marked as shared;
//  - uploaded only after the server re-checks the person's rights: the upload is the ordinary document inbox upload, which the API authorises like any other, and a refusal drops the receipt
//    with its reason; it is deduplicated by a client-generated ID (the Idempotency-Key) and the file's SHA-256, which the server compares.
// The storage is a port so the logic is tested with no browser: the IndexedDB store below is the browser's.

export interface Sealed {
  id: string; // client-generated: the Idempotency-Key of its upload
  user: string; // the person who captured it: nobody else sees or uploads it
  name: string;
  type: string;
  size: number;
  sha256: string; // of the plain file
  iv: Uint8Array;
  data: ArrayBuffer; // the sealed file
  queuedAt: number;
  expiresAt: number;
}

export interface Store {
  key(): Promise<CryptoKey | undefined>;
  setKey(k: CryptoKey): Promise<void>;
  all(): Promise<Sealed[]>;
  put(s: Sealed): Promise<void>;
  remove(id: string): Promise<void>;
  clear(): Promise<void>;
}

export const DEFAULT_HOURS = 72;
export const MAX_QUEUE = 50;
export const MAX_BYTES = 20 * 1024 * 1024;

export class QueueRefused extends Error {
  constructor(
    public code: "shared-device" | "too-big" | "full" | "unsupported",
    message: string,
  ) {
    super(message);
  }
}

// memoryStore is a store in memory: the tests', and a browser's fallback that keeps nothing past the page.
export function memoryStore(): Store {
  let key: CryptoKey | undefined;
  const items = new Map<string, Sealed>();
  return {
    key: async () => key,
    setKey: async (k) => void (key = k),
    all: async () => [...items.values()],
    put: async (s) => void items.set(s.id, s),
    remove: async (id) => void items.delete(id),
    clear: async () => void items.clear(),
  };
}

const DB = "fyniq-offline";

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const r = indexedDB.open(DB, 1);
    r.onupgradeneeded = () => {
      r.result.createObjectStore("receipts", { keyPath: "id" });
      r.result.createObjectStore("keys");
    };
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(r.error);
  });
}

function request<T>(db: IDBDatabase, store: string, mode: IDBTransactionMode, run: (s: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    const r = run(db.transaction(store, mode).objectStore(store));
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(r.error);
  });
}

// indexedDbStore is the browser's store: the sealed receipts and the device's key in IndexedDB.
export function indexedDbStore(): Store {
  return {
    key: async () => request<CryptoKey | undefined>(await open(), "keys", "readonly", (s) => s.get("device")),
    setKey: async (k) => void (await request(await open(), "keys", "readwrite", (s) => s.put(k, "device"))),
    all: async () => request<Sealed[]>(await open(), "receipts", "readonly", (s) => s.getAll()),
    put: async (v) => void (await request(await open(), "receipts", "readwrite", (s) => s.put(v))),
    remove: async (id) => void (await request(await open(), "receipts", "readwrite", (s) => s.delete(id))),
    clear: async () => void (await request(await open(), "receipts", "readwrite", (s) => s.clear())),
  };
}

const SHARED = "fyniq.shared-device";

// isSharedDevice says whether the person marked this device as shared: nothing is kept on it.
export function isSharedDevice(): boolean {
  try {
    return localStorage.getItem(SHARED) === "1";
  } catch {
    return true; // storage that refuses is not a place to keep a receipt
  }
}

// setSharedDevice marks this device shared or not; marking it shared at once removes what was queued.
export async function setSharedDevice(shared: boolean, store: Store = indexedDbStore()): Promise<void> {
  try {
    if (shared) localStorage.setItem(SHARED, "1");
    else localStorage.removeItem(SHARED);
  } catch {
    /* storage refused: isSharedDevice then says shared */
  }
  if (shared) await store.clear();
}

const hex = (b: ArrayBuffer) => [...new Uint8Array(b)].map((x) => x.toString(16).padStart(2, "0")).join("");

// sha256 is the SHA-256 of bytes, in hex.
export async function sha256(bytes: ArrayBuffer): Promise<string> {
  return hex(await crypto.subtle.digest("SHA-256", bytes));
}

export interface QueuedMeta {
  id: string;
  name: string;
  type: string;
  size: number;
  sha256: string;
  queuedAt: number;
  expiresAt: number;
}

const meta = (s: Sealed): QueuedMeta => ({ id: s.id, name: s.name, type: s.type, size: s.size, sha256: s.sha256, queuedAt: s.queuedAt, expiresAt: s.expiresAt });

// Outcome is what an upload answered for one receipt.
export type Outcome = { kind: "uploaded" } | { kind: "duplicate" } | { kind: "retry" } | { kind: "refused"; reason: string };

export interface Flushed {
  uploaded: number;
  duplicates: number;
  refused: { name: string; reason: string }[];
  left: number;
}

export class Queue {
  constructor(
    private store: Store,
    private user: string,
    private hours: number = DEFAULT_HOURS,
    private now: () => number = Date.now,
    private shared: () => boolean = isSharedDevice,
  ) {}

  private async key(): Promise<CryptoKey> {
    let k = await this.store.key();
    if (!k) {
      k = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
      await this.store.setKey(k);
    }
    return k;
  }

  // enqueue seals a file and keeps it until there is a signal, up to its time limit.
  async enqueue(file: File): Promise<QueuedMeta> {
    if (!globalThis.crypto?.subtle) throw new QueueRefused("unsupported", "This browser cannot keep a receipt safely, so it is not kept.");
    if (this.shared()) throw new QueueRefused("shared-device", "This device is marked as shared, so no receipt is kept on it. Capture it again when you are back online.");
    if (file.size > MAX_BYTES) throw new QueueRefused("too-big", "The file is over 20 MB, which cannot wait on the device. Capture it again when you are back online.");
    if ((await this.list()).length >= MAX_QUEUE) throw new QueueRefused("full", "Fifty receipts are waiting already. Upload them when you are back online first.");
    const plain = await file.arrayBuffer();
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const data = await crypto.subtle.encrypt({ name: "AES-GCM", iv }, await this.key(), plain);
    const at = this.now();
    const s: Sealed = { id: crypto.randomUUID(), user: this.user, name: file.name, type: file.type, size: file.size, sha256: await sha256(plain), iv, data, queuedAt: at, expiresAt: at + this.hours * 3600_000 };
    await this.store.put(s);
    return meta(s);
  }

  // list is what waits for this person, the oldest first; a receipt past its time is dropped as it is looked at.
  async list(): Promise<QueuedMeta[]> {
    const at = this.now();
    const mine: Sealed[] = [];
    for (const s of await this.store.all()) {
      if (s.expiresAt <= at) await this.store.remove(s.id);
      else if (s.user === this.user) mine.push(s);
    }
    return mine.sort((a, b) => a.queuedAt - b.queuedAt).map(meta);
  }

  async remove(id: string): Promise<void> {
    const s = (await this.store.all()).find((x) => x.id === id);
    if (s && s.user === this.user) await this.store.remove(id);
  }

  // clear removes every receipt on the device: at sign-out, and when the device is marked shared.
  async clear(): Promise<void> {
    await this.store.clear();
  }

  private async open(s: Sealed): Promise<File> {
    const plain = await crypto.subtle.decrypt({ name: "AES-GCM", iv: s.iv as BufferSource }, await this.key(), s.data);
    return new File([plain], s.name, { type: s.type });
  }

  // flush uploads what waits, one at a time, oldest first. upload is the document inbox's own upload with the receipt's ID as its Idempotency-Key: the API checks the person's rights and
  // the file's hash, so a receipt the person may no longer capture is refused there, and one already in the inbox answers duplicate. A retry stops the run, leaving the rest queued.
  async flush(upload: (file: File, id: string, sha256: string) => Promise<Outcome>): Promise<Flushed> {
    const out: Flushed = { uploaded: 0, duplicates: 0, refused: [], left: 0 };
    const all = (await this.store.all()).filter((s) => s.user === this.user).sort((a, b) => a.queuedAt - b.queuedAt);
    for (const s of all) {
      if (s.expiresAt <= this.now()) {
        await this.store.remove(s.id);
        continue;
      }
      let result: Outcome;
      try {
        result = await upload(await this.open(s), s.id, s.sha256);
      } catch {
        result = { kind: "retry" };
      }
      if (result.kind === "retry") break;
      await this.store.remove(s.id);
      if (result.kind === "uploaded") out.uploaded++;
      else if (result.kind === "duplicate") out.duplicates++;
      else out.refused.push({ name: s.name, reason: result.reason });
    }
    out.left = (await this.list()).length;
    return out;
  }
}
