import * as Y from "yjs";
import {
  Awareness,
  applyAwarenessUpdate,
  encodeAwarenessUpdate,
  removeAwarenessStates,
} from "y-protocols/awareness";
import { FrameType, encodeFrame, parseFrame } from "./protocol";

export type ConnectionStatus = "connecting" | "syncing" | "synced" | "offline";

type Listener = (s: ProviderState) => void;

export interface ProviderState {
  status: ConnectionStatus;
  /** Local updates made since the socket last went down. */
  pendingOffline: number;
  reconnects: number;
}

/**
 * Connects a Y.Doc to the relay server.
 *
 * Offline edits: while the socket is down, updates keep landing in the Y.Doc
 * (and in IndexedDB, via y-indexeddb in useCanvas), so nothing is lost on a
 * reload. On reconnect, after the server has streamed the room's history, we
 * send our entire state as one update. The server can't compute a diff
 * because it never decodes Yjs, and Yjs makes re-sending known operations a
 * no-op. That is what guarantees full recovery after a drop. Even updates
 * that were in flight when the socket died get re-sent.
 */
export class CanvasProvider {
  readonly awareness: Awareness;
  private ws: WebSocket | null = null;
  private backoff = 250;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private destroyed = false;
  private listeners = new Set<Listener>();
  private state: ProviderState = { status: "connecting", pendingOffline: 0, reconnects: 0 };

  constructor(
    private readonly url: string,
    private readonly room: string,
    readonly doc: Y.Doc,
  ) {
    this.awareness = new Awareness(doc);
    doc.on("update", this.onDocUpdate);
    this.awareness.on("update", this.onAwarenessUpdate);
    window.addEventListener("beforeunload", this.onUnload);
    this.connect();
  }

  subscribe(fn: Listener): () => void {
    this.listeners.add(fn);
    fn(this.state);
    return () => this.listeners.delete(fn);
  }

  destroy(): void {
    this.destroyed = true;
    if (this.timer) clearTimeout(this.timer);
    this.doc.off("update", this.onDocUpdate);
    this.awareness.off("update", this.onAwarenessUpdate);
    window.removeEventListener("beforeunload", this.onUnload);
    removeAwarenessStates(this.awareness, [this.doc.clientID], "local");
    this.ws?.close();
  }

  private set(patch: Partial<ProviderState>) {
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((l) => l(this.state));
  }

  private connect() {
    if (this.destroyed) return;
    this.set({ status: "connecting" });
    const ws = new WebSocket(`${this.url}/ws/${encodeURIComponent(this.room)}`);
    ws.binaryType = "arraybuffer";
    this.ws = ws;

    ws.onopen = () => {
      this.backoff = 250;
      this.set({ status: "syncing" });
    };

    ws.onmessage = (ev: MessageEvent<ArrayBuffer>) => {
      const frame = parseFrame(ev.data);
      if (!frame) return;
      switch (frame.type) {
        case FrameType.Update:
          Y.applyUpdate(this.doc, frame.payload, this);
          break;
        case FrameType.Awareness:
          applyAwarenessUpdate(this.awareness, frame.payload, this);
          break;
        case FrameType.Synced:
          // Server history is in. Push everything we have that it might not.
          this.send(FrameType.Update, Y.encodeStateAsUpdate(this.doc));
          this.send(
            FrameType.Awareness,
            encodeAwarenessUpdate(this.awareness, [this.doc.clientID]),
          );
          this.set({ status: "synced", pendingOffline: 0 });
          break;
      }
    };

    ws.onclose = () => {
      this.ws = null;
      // Peers' cursors are stale once we can't hear them.
      const others = [...this.awareness.getStates().keys()].filter((id) => id !== this.doc.clientID);
      removeAwarenessStates(this.awareness, others, this);
      if (this.destroyed) return;
      this.set({ status: "offline", reconnects: this.state.reconnects + 1 });
      // Exponential backoff with full jitter, capped at 10 s, so a server
      // restart isn't hit by every tab in the same millisecond.
      const delay = Math.random() * this.backoff;
      this.backoff = Math.min(this.backoff * 2, 10_000);
      this.timer = setTimeout(() => this.connect(), delay);
    };

    ws.onerror = () => ws.close();
  }

  private send(type: FrameType, payload: Uint8Array): boolean {
    if (this.ws?.readyState !== WebSocket.OPEN) return false;
    this.ws.send(encodeFrame(type, payload));
    return true;
  }

  private onDocUpdate = (update: Uint8Array, origin: unknown) => {
    if (origin === this) return; // came from the server; don't echo
    if (this.state.status !== "synced" || !this.send(FrameType.Update, update)) {
      this.set({ pendingOffline: this.state.pendingOffline + 1 });
    }
  };

  private onAwarenessUpdate = (
    { added, updated, removed }: { added: number[]; updated: number[]; removed: number[] },
    origin: unknown,
  ) => {
    if (origin === this) return;
    const changed = added.concat(updated, removed);
    this.send(FrameType.Awareness, encodeAwarenessUpdate(this.awareness, changed));
  };

  private onUnload = () => {
    removeAwarenessStates(this.awareness, [this.doc.clientID], "unload");
  };
}
