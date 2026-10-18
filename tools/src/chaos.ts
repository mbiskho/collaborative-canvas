// Convergence under connection drops.
//
// Simulates N editors in one room, each with its own Y.Doc and WebSocket.
// They make random edits (add, move, recolour, type) while their connections
// are randomly killed and restored. Then all edits stop, everyone
// reconnects, and the script checks that every replica holds byte-identical
// state. That check is the "100% vector state recovery after connection drop"
// number in the README.
//
//   URL=ws://localhost:8080 CLIENTS=8 SECONDS=60 DROP_EVERY=5 npm run chaos

import * as Y from "yjs";

const URL = process.env.URL ?? "ws://localhost:8080";
const CLIENTS = Number(process.env.CLIENTS ?? 8);
const SECONDS = Number(process.env.SECONDS ?? 60);
const OPS_PER_SEC = Number(process.env.OPS_PER_SEC ?? 10);
const DROP_EVERY = Number(process.env.DROP_EVERY ?? 5); // mean seconds between drops per client
const ROOM = process.env.ROOM ?? `chaos-${Date.now().toString(36)}`;

const UPDATE = 0x01;
const SYNCED = 0x03;

class Replica {
  doc = new Y.Doc();
  ws: WebSocket | null = null;
  synced = false;
  ops = 0;
  drops = 0;
  private stopped = false;

  constructor(readonly n: number) {
    this.doc.on("update", (u: Uint8Array, origin: unknown) => {
      if (origin !== this && this.synced) this.send(u);
    });
  }

  connect(): Promise<void> {
    return new Promise((resolve) => {
      const ws = new WebSocket(`${URL}/ws/${ROOM}`);
      ws.binaryType = "arraybuffer";
      this.ws = ws;
      this.synced = false;
      ws.onmessage = (ev) => {
        const b = new Uint8Array(ev.data as ArrayBuffer);
        if (b[0] === UPDATE) Y.applyUpdate(this.doc, b.subarray(1), this);
        if (b[0] === SYNCED) {
          this.synced = true;
          this.send(Y.encodeStateAsUpdate(this.doc)); // same recovery path as the browser
          resolve();
        }
      };
      ws.onclose = () => { this.synced = false; this.ws = null; };
      ws.onerror = () => ws.close();
    });
  }

  drop(): void {
    this.drops++;
    this.ws?.close();
  }

  private send(u: Uint8Array) {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    const f = new Uint8Array(u.length + 1);
    f[0] = UPDATE;
    f.set(u, 1);
    this.ws.send(f);
  }

  edit(): void {
    const shapes = this.doc.getMap<Y.Map<unknown>>("shapes");
    const ids = [...shapes.keys()];
    const r = Math.random();
    this.doc.transact(() => {
      if (ids.length < 5 || r < 0.2) {
        const m = new Y.Map<unknown>();
        m.set("type", "text");
        m.set("x", rnd(800)); m.set("y", rnd(600)); m.set("z", rnd(1000));
        const t = new Y.Text(); t.insert(0, `c${this.n}`); m.set("text", t);
        shapes.set(`${this.n}-${this.ops}`, m);
      } else {
        const m = shapes.get(ids[rnd(ids.length)])!;
        if (r < 0.6) { m.set("x", rnd(800)); m.set("y", rnd(600)); }
        else if (r < 0.75) m.set("fill", `#${rnd(0xffffff).toString(16).padStart(6, "0")}`);
        else if (r < 0.97) { const t = m.get("text") as Y.Text; t.insert(rnd(t.length + 1), String.fromCharCode(97 + rnd(26))); }
        else shapes.delete(ids[rnd(ids.length)]);
      }
    });
    this.ops++;
  }

  async run(until: number) {
    while (Date.now() < until && !this.stopped) {
      this.edit(); // offline edits are part of the test
      if (Math.random() < 1 / (DROP_EVERY * OPS_PER_SEC) && this.ws) {
        this.drop();
        setTimeout(() => void this.connect(), 200 + rnd(3000));
      }
      await sleep(1000 / OPS_PER_SEC);
    }
  }
}

const rnd = (n: number) => Math.floor(Math.random() * n);
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const hex = (b: Uint8Array) => Buffer.from(b).toString("hex");

async function main() {
  const replicas = Array.from({ length: CLIENTS }, (_, i) => new Replica(i));
  await Promise.all(replicas.map((r) => r.connect()));
  console.error(`room ${ROOM}: ${CLIENTS} clients, ${SECONDS}s, ~${OPS_PER_SEC} ops/s each, drop every ~${DROP_EVERY}s`);

  const until = Date.now() + SECONDS * 1000;
  await Promise.all(replicas.map((r) => r.run(until)));

  // Quiesce: reconnect anyone offline, then wait for state vectors to agree.
  const settleStart = Date.now();
  await Promise.all(replicas.filter((r) => !r.synced).map((r) => r.connect()));
  let converged = false;
  while (Date.now() - settleStart < 30_000) {
    const vectors = new Set(replicas.map((r) => hex(Y.encodeStateVector(r.doc))));
    if (vectors.size === 1) { converged = true; break; }
    await sleep(100);
  }
  const contents = new Set(replicas.map((r) => JSON.stringify(r.doc.getMap("shapes").toJSON())));

  const result = {
    room: ROOM,
    clients: CLIENTS,
    seconds: SECONDS,
    ops: replicas.reduce((n, r) => n + r.ops, 0),
    drops: replicas.reduce((n, r) => n + r.drops, 0),
    shapes: replicas[0].doc.getMap("shapes").size,
    converged: converged && contents.size === 1,
    settle_ms: Date.now() - settleStart,
  };
  console.log(JSON.stringify(result, null, 2));
  replicas.forEach((r) => r.ws?.close());
  process.exit(result.converged ? 0 : 1);
}

void main();
