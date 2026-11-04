// The website's VRChat client. It runs VRCove's VRChat routes compiled to
// WebAssembly (cmd/vrcove-wasm), which dial VRChat over TLS through
// /api/relay, so VRCove's server never sees VRChat passwords, tokens or
// responses. VRChat session cookies stay in this browser's IndexedDB,
// encrypted with a key that cannot be exported.
//
// It runs as a SharedWorker where the browser has one, so tabs share one
// connection, and as a dedicated worker elsewhere.

import type { WorkerReply, WorkerRequest, WorkerResponse } from './vrchat-client';
// Go's glue for the WebAssembly client, from the toolchain that built it
// (scripts/build-wasm.mjs). It defines globalThis.Go.
import './generated/wasm_exec.js';

type Go = { importObject: WebAssembly.Imports; run(instance: WebAssembly.Instance): Promise<void> };
type Port = {
  postMessage(message: WorkerReply, transfer?: Transferable[]): void;
  onmessage: ((event: MessageEvent) => void) | null;
};
type Bytes = Uint8Array<ArrayBuffer>;
type Sealed = { iv: Bytes; data: ArrayBuffer };

const scope = globalThis as unknown as {
  Go: new () => Go;
  vrcoveConfig?: { origin: string; sessionKey: Uint8Array };
  vrcoveHandle?: (request: WorkerRequest, onChunk: (chunk: Uint8Array) => void) => Promise<WorkerResponse>;
  vrcoveCancel?: (id: number) => void;
  onconnect?: (event: MessageEvent) => void;
  SharedWorkerGlobalScope?: unknown;
  location: Location;
};

const dbName = 'vrcove-vrchat';
const store = 'state';

function request<T>(r: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(r.error);
  });
}

async function openDB(): Promise<IDBDatabase> {
  const open = indexedDB.open(dbName, 1);
  open.onupgradeneeded = () => open.result.createObjectStore(store);
  return request(open);
}

const read = <T>(db: IDBDatabase, key: string) =>
  request(db.transaction(store).objectStore(store).get(key)) as Promise<T | undefined>;
const write = (db: IDBDatabase, key: string, value: unknown) =>
  request(db.transaction(store, 'readwrite').objectStore(store).put(value, key));

async function seal(key: CryptoKey, plain: Bytes): Promise<Sealed> {
  const iv = crypto.getRandomValues(new Uint8Array(12));
  return { iv, data: await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, plain) };
}

async function unseal(key: CryptoKey, sealed: Sealed): Promise<Bytes> {
  return new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: sealed.iv }, key, sealed.data));
}

// start loads or creates the keys, then runs the WebAssembly client.
async function start() {
  const db = await openDB();
  let wrap = await read<CryptoKey>(db, 'wrap');
  if (!wrap) {
    wrap = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']);
    await write(db, 'wrap', wrap);
  }
  const savedKey = await read<Sealed>(db, 'session');
  let sessionKey: Bytes;
  if (savedKey) {
    sessionKey = await unseal(wrap, savedKey);
  } else {
    sessionKey = crypto.getRandomValues(new Uint8Array(32));
    await write(db, 'session', await seal(wrap, sessionKey));
  }
  const go = new scope.Go();
  scope.vrcoveConfig = { origin: scope.location.origin, sessionKey };
  const integrity = import.meta.env.VRCOVE_WASM_INTEGRITY;
  const { instance } = await WebAssembly.instantiateStreaming(
    fetch('/vrcove/vrcove.wasm', integrity ? { integrity } : {}),
    go.importObject,
  );
  void go.run(instance);
  for (let i = 0; !scope.vrcoveHandle; i++) {
    if (i > 100) throw new Error('VRChat client did not start');
    await new Promise((r) => setTimeout(r, 10));
  }
  return { db, wrap };
}

const ready = start();

type Jar = Record<string, string>;

async function loadJar(db: IDBDatabase, wrap: CryptoKey): Promise<Jar> {
  const sealed = await read<Sealed>(db, 'jar');
  return sealed ? JSON.parse(new TextDecoder().decode(await unseal(wrap, sealed))) : {};
}

// Cookie updates run one at a time, each on the latest saved jar.
let jarQueue: Promise<unknown> = Promise.resolve();

function saveCookies(db: IDBDatabase, wrap: CryptoKey, setCookies: string[]) {
  const next = jarQueue.then(async () => {
    const jar = await loadJar(db, wrap);
    for (const header of setCookies) {
      const [pair, ...attributes] = header.split(';');
      const eq = pair.indexOf('=');
      if (eq < 1) continue;
      const name = pair.slice(0, eq).trim();
      const value = pair.slice(eq + 1).trim();
      const expired = attributes.some((a) => {
        const [k, v = ''] = a.split('=').map((s) => s.trim());
        if (k.toLowerCase() === 'max-age') return Number(v) <= 0;
        if (k.toLowerCase() === 'expires') return Date.parse(v) <= Date.now();
        return false;
      });
      if (expired || value === '') delete jar[name];
      else jar[name] = value;
    }
    await write(db, 'jar', await seal(wrap, new TextEncoder().encode(JSON.stringify(jar))));
  });
  jarQueue = next.catch(() => {});
  return next;
}

function serve(port: Port) {
  port.onmessage = async (event: MessageEvent) => {
    const message = event.data as { id: number; request?: WorkerRequest; cancel?: boolean };
    const { id } = message;
    if (message.cancel) {
      scope.vrcoveCancel?.(id);
      return;
    }
    try {
      const { db, wrap } = await ready;
      const jar = await loadJar(db, wrap);
      const headers = message.request!.headers.filter(([name]) => name.toLowerCase() !== 'cookie');
      const cookie = Object.entries(jar)
        .map(([name, value]) => `${name}=${value}`)
        .join('; ');
      if (cookie) headers.push(['Cookie', cookie]);
      const response = await scope.vrcoveHandle!({ ...message.request!, id, headers }, (chunk) =>
        port.postMessage({ id, chunk }, [chunk.buffer]),
      );
      const setCookies = response.headers.filter(([name]) => name.toLowerCase() === 'set-cookie');
      if (setCookies.length)
        await saveCookies(
          db,
          wrap,
          setCookies.map(([, value]) => value),
        );
      const visible = response.headers.filter(([name]) => name.toLowerCase() !== 'set-cookie');
      port.postMessage({ id, response: { ...response, headers: visible } }, [response.body.buffer]);
    } catch (e) {
      port.postMessage({ id, error: e instanceof Error ? e.message : String(e) });
    }
  };
}

if (scope.SharedWorkerGlobalScope && globalThis instanceof (scope.SharedWorkerGlobalScope as typeof Object)) {
  scope.onconnect = (event: MessageEvent) => serve(event.ports[0] as unknown as Port);
} else {
  serve(globalThis as unknown as Port);
}
