import { useEffect, useState } from "react";
import { Canvas, type Tool } from "./components/Canvas";
import { Toolbar } from "./components/Toolbar";
import { PresenceBar } from "./components/PresenceBar";
import { useCanvas } from "./hooks/useCanvas";
import { deleteShape, setText } from "./lib/schema";

function roomFromUrl(): string {
  const r = new URLSearchParams(location.search).get("room");
  if (r && /^[A-Za-z0-9_-]{1,64}$/.test(r)) return r;
  const fresh = Math.random().toString(36).slice(2, 10);
  history.replaceState(null, "", `?room=${fresh}`);
  return fresh;
}

export default function App() {
  const [room] = useState(roomFromUrl);
  const { doc, shapes, peers, conn, provider, undo } = useCanvas(room);
  const [tool, setTool] = useState<Tool>("select");
  const [color, setColor] = useState("#bfdbfe");
  const [selected, setSelected] = useState<string | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const [, force] = useState(0);

  useEffect(() => {
    const bump = () => force((n) => n + 1);
    undo.on("stack-item-added", bump);
    undo.on("stack-item-popped", bump);
    return () => {
      undo.off("stack-item-added", bump);
      undo.off("stack-item-popped", bump);
    };
  }, [undo]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (editing) return;
      const mod = e.metaKey || e.ctrlKey;
      if (mod && e.key === "z") { e.preventDefault(); e.shiftKey ? undo.redo() : undo.undo(); return; }
      if ((e.key === "Delete" || e.key === "Backspace") && selected) { deleteShape(doc, selected); setSelected(null); return; }
      const map: Record<string, Tool> = { v: "select", r: "rect", o: "ellipse", t: "text" };
      if (map[e.key]) setTool(map[e.key]);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [doc, editing, selected, undo]);

  const editShape = editing ? shapes.find((s) => s.id === editing) : undefined;

  return (
    <div className="flex h-screen flex-col">
      <header className="flex items-center justify-between border-b border-neutral-200 bg-white px-4 py-2">
        <span className="font-semibold text-neutral-800">Canvas</span>
        <Toolbar
          tool={tool} color={color} canUndo={undo.canUndo()} canRedo={undo.canRedo()}
          onTool={setTool} onColor={setColor} onUndo={() => undo.undo()} onRedo={() => undo.redo()}
          onDelete={() => { if (selected) { deleteShape(doc, selected); setSelected(null); } }}
        />
        <PresenceBar peers={peers} conn={conn} room={room} />
      </header>
      <main className="relative flex-1">
        <Canvas
          doc={doc} awareness={provider?.awareness} shapes={shapes} peers={peers} tool={tool} color={color}
          selected={selected} onSelect={setSelected} onEditText={setEditing} onToolDone={() => setTool("select")}
        />
        {editShape && (
          <textarea
            autoFocus
            defaultValue={editShape.text}
            onChange={(e) => setText(doc, editShape.id, e.target.value)}
            onBlur={() => setEditing(null)}
            className="absolute resize-none rounded border border-blue-500 bg-white/90 p-1 text-[18px] outline-none"
            style={{ left: editShape.x - 4, top: editShape.y - 4, minWidth: Math.max(editShape.w, 160), height: 40 }}
          />
        )}
      </main>
    </div>
  );
}
