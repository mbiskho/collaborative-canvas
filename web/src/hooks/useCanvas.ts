import { useEffect, useMemo, useState } from "react";
import * as Y from "yjs";
import { IndexeddbPersistence } from "y-indexeddb";
import { CanvasProvider, type ProviderState } from "../lib/provider";
import { LOCAL, readShapes, shapesMap, type Shape } from "../lib/schema";

export interface Peer {
  clientId: number;
  name: string;
  color: string;
  cursor?: { x: number; y: number };
  selection?: string | null;
}

const COLORS = ["#e11d48", "#2563eb", "#16a34a", "#d97706", "#7c3aed", "#0891b2", "#db2777"];
const NAMES = ["Ada", "Grace", "Linus", "Barbara", "Ken", "Margaret", "Dennis", "Radia"];

function wsBase(): string {
  const configured = import.meta.env.VITE_WS_URL as string | undefined;
  if (configured) return configured;
  const proto = location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${location.host}`;
}

export function useCanvas(room: string) {
  const doc = useMemo(() => new Y.Doc(), [room]);
  const [shapes, setShapes] = useState<Shape[]>([]);
  const [peers, setPeers] = useState<Peer[]>([]);
  const [conn, setConn] = useState<ProviderState>({ status: "connecting", pendingOffline: 0, reconnects: 0 });
  const [provider, setProvider] = useState<CanvasProvider | null>(null);

  const undo = useMemo(
    () => new Y.UndoManager(shapesMap(doc), { trackedOrigins: new Set([LOCAL]), captureTimeout: 400 }),
    [doc],
  );

  useEffect(() => {
    // IndexedDB first: offline edits survive a reload, and the canvas paints
    // from local state before the server answers.
    const idb = new IndexeddbPersistence(`canvas:${room}`, doc);
    const p = new CanvasProvider(wsBase(), room, doc);
    setProvider(p);

    const me = Math.abs(doc.clientID);
    p.awareness.setLocalStateField("user", {
      name: NAMES[me % NAMES.length],
      color: COLORS[me % COLORS.length],
    });

    // Re-read shapes at most once per animation frame. A drag produces an
    // update per pointer move; React doesn't need to hear every one.
    let raf = 0;
    const onChange = () => {
      if (raf) return;
      raf = requestAnimationFrame(() => {
        raf = 0;
        setShapes(readShapes(doc));
      });
    };
    shapesMap(doc).observeDeep(onChange);
    onChange();

    const onAwareness = () => {
      const list: Peer[] = [];
      p.awareness.getStates().forEach((s, clientId) => {
        if (clientId === doc.clientID || !s.user) return;
        list.push({ clientId, ...s.user, cursor: s.cursor, selection: s.selection });
      });
      setPeers(list);
    };
    p.awareness.on("change", onAwareness);
    const unsub = p.subscribe(setConn);

    return () => {
      cancelAnimationFrame(raf);
      shapesMap(doc).unobserveDeep(onChange);
      p.awareness.off("change", onAwareness);
      unsub();
      p.destroy();
      idb.destroy();
      undo.destroy();
      doc.destroy();
    };
  }, [doc, room, undo]);

  return { doc, shapes, peers, conn, provider, undo };
}
