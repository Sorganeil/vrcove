// The page's side of the VRChat worker (vrchat-worker.ts). workerFetch answers
// /api/account requests like fetch, and workerStream reads an event stream such
// as live notifications.

export type WorkerRequest = {
  id?: number;
  method: string;
  path: string;
  headers: [string, string][];
  body?: Uint8Array;
};
export type WorkerResponse = { status: number; headers: [string, string][]; body: Uint8Array };
export type WorkerReply =
  | { id: number; chunk: Uint8Array }
  | { id: number; response: WorkerResponse }
  | { id: number; error: string };

type Port = { postMessage(message: unknown, transfer?: Transferable[]): void };
type Pending = {
  resolve: (r: WorkerResponse) => void;
  reject: (e: Error) => void;
  onChunk?: (chunk: Uint8Array) => void;
};

let port: Port | null = null;
let nextID = 1;
const pending = new Map<number, Pending>();

function receive(event: MessageEvent<WorkerReply>) {
  const reply = event.data;
  const waiting = pending.get(reply.id);
  if (!waiting) return;
  if ('chunk' in reply) {
    waiting.onChunk?.(reply.chunk);
    return;
  }
  pending.delete(reply.id);
  if ('error' in reply) waiting.reject(new Error(reply.error));
  else waiting.resolve(reply.response);
}

function connect(): Port {
  if (port) return port;
  // One SharedWorker serves every tab. Chrome on Android has none, so each
  // tab starts its own worker there.
  if (typeof SharedWorker !== 'undefined') {
    const worker = new SharedWorker(new URL('./vrchat-worker.ts', import.meta.url), {
      type: 'module',
      name: 'vrcove-vrchat',
    });
    worker.port.onmessage = receive;
    worker.port.start();
    port = worker.port;
  } else {
    const worker = new Worker(new URL('./vrchat-worker.ts', import.meta.url), { type: 'module' });
    worker.onmessage = receive;
    port = worker;
  }
  return port;
}

function send(request: WorkerRequest, onChunk?: (chunk: Uint8Array) => void) {
  const id = nextID++;
  const result = new Promise<WorkerResponse>((resolve, reject) => {
    pending.set(id, { resolve, reject, onChunk });
  });
  connect().postMessage({ id, request });
  return { id, result };
}

function headerPairs(headers?: HeadersInit): [string, string][] {
  return [...new Headers(headers).entries()];
}

async function bodyBytes(body?: BodyInit | null): Promise<Uint8Array | undefined> {
  if (body == null) return undefined;
  return new Uint8Array(await new Response(body).arrayBuffer());
}

// workerFetch answers a same-origin /api/account request from the worker.
export async function workerFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const { result } = send({
    method: init.method || 'GET',
    path,
    headers: headerPairs(init.headers),
    body: await bodyBytes(init.body),
  });
  const response = await result;
  const status = response.status;
  // Response cannot carry a body for these statuses.
  const empty = status === 204 || status === 304;
  return new Response(empty ? null : (response.body as BlobPart as BodyInit), {
    status,
    headers: response.headers,
  });
}

// workerStream reads an event stream from the worker until close is called.
// onEnd runs when the stream ends on its own, with the final status.
export function workerStream(
  path: string,
  onText: (text: string) => void,
  onEnd: (status: number) => void,
): { close(): void } {
  const decoder = new TextDecoder();
  const { id, result } = send({ method: 'GET', path, headers: [['Accept', 'text/event-stream']] }, (chunk) =>
    onText(decoder.decode(chunk, { stream: true })),
  );
  let closed = false;
  result.then(
    (r) => {
      if (!closed) onEnd(r.status);
    },
    () => {
      if (!closed) onEnd(0);
    },
  );
  return {
    close() {
      closed = true;
      pending.delete(id);
      connect().postMessage({ id, cancel: true });
    },
  };
}
