import type { Tool } from "./Canvas";

const TOOLS: { id: Tool; label: string; key: string }[] = [
  { id: "select", label: "Select", key: "V" },
  { id: "rect", label: "Rectangle", key: "R" },
  { id: "ellipse", label: "Ellipse", key: "O" },
  { id: "text", label: "Text", key: "T" },
];

const SWATCHES = ["#fde68a", "#bfdbfe", "#bbf7d0", "#fecaca", "#e9d5ff", "#f3f4f6"];

interface Props {
  tool: Tool;
  color: string;
  canUndo: boolean;
  canRedo: boolean;
  onTool: (t: Tool) => void;
  onColor: (c: string) => void;
  onUndo: () => void;
  onRedo: () => void;
  onDelete: () => void;
}

export function Toolbar(p: Props) {
  return (
    <div className="flex items-center gap-1 rounded-xl border border-neutral-200 bg-white p-1.5 shadow-sm">
      {TOOLS.map((t) => (
        <button
          key={t.id}
          title={`${t.label} (${t.key})`}
          onClick={() => p.onTool(t.id)}
          className={`rounded-lg px-3 py-1.5 text-sm ${p.tool === t.id ? "bg-blue-600 text-white" : "text-neutral-700 hover:bg-neutral-100"}`}
        >
          {t.label}
        </button>
      ))}
      <div className="mx-1 h-6 w-px bg-neutral-200" />
      {SWATCHES.map((c) => (
        <button
          key={c}
          aria-label={`colour ${c}`}
          onClick={() => p.onColor(c)}
          className={`h-6 w-6 rounded-full border ${p.color === c ? "ring-2 ring-blue-600 ring-offset-1" : "border-neutral-300"}`}
          style={{ background: c }}
        />
      ))}
      <div className="mx-1 h-6 w-px bg-neutral-200" />
      <button disabled={!p.canUndo} onClick={p.onUndo} className="rounded-lg px-2 py-1.5 text-sm text-neutral-700 enabled:hover:bg-neutral-100 disabled:opacity-40">Undo</button>
      <button disabled={!p.canRedo} onClick={p.onRedo} className="rounded-lg px-2 py-1.5 text-sm text-neutral-700 enabled:hover:bg-neutral-100 disabled:opacity-40">Redo</button>
      <button onClick={p.onDelete} className="rounded-lg px-2 py-1.5 text-sm text-red-600 hover:bg-red-50">Delete</button>
    </div>
  );
}
