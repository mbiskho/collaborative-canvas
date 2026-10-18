import { useRef, useState, type PointerEvent } from "react";
import type * as Y from "yjs";
import type { Awareness } from "y-protocols/awareness";
import { addShape, bringToFront, updateShape, type Shape, type ShapeType } from "../lib/schema";
import type { Peer } from "../hooks/useCanvas";

export type Tool = "select" | ShapeType;

interface Props {
  doc: Y.Doc;
  awareness: Awareness | undefined;
  shapes: Shape[];
  peers: Peer[];
  tool: Tool;
  color: string;
  selected: string | null;
  onSelect: (id: string | null) => void;
  onEditText: (id: string) => void;
  onToolDone: () => void;
}

type Drag =
  | { kind: "move"; id: string; dx: number; dy: number }
  | { kind: "create"; id: string; x0: number; y0: number }
  | null;

const CURSOR_HZ = 30; // matches the server's awareness rate limit

export function Canvas(p: Props) {
  const svg = useRef<SVGSVGElement>(null);
  const [drag, setDrag] = useState<Drag>(null);
  const lastCursor = useRef(0);

  const point = (e: PointerEvent) => {
    const r = svg.current!.getBoundingClientRect();
    return { x: Math.round(e.clientX - r.left), y: Math.round(e.clientY - r.top) };
  };

  const onDown = (e: PointerEvent<SVGSVGElement>) => {
    const { x, y } = point(e);
    (e.target as Element).setPointerCapture?.(e.pointerId);
    if (p.tool === "select") {
      const id = (e.target as SVGElement).dataset.shape ?? null;
      p.onSelect(id);
      p.awareness?.setLocalStateField("selection", id);
      if (id) {
        const s = p.shapes.find((s) => s.id === id)!;
        bringToFront(p.doc, id);
        setDrag({ kind: "move", id, dx: x - s.x, dy: y - s.y });
      }
      return;
    }
    const id = addShape(p.doc, {
      type: p.tool, x, y, w: 1, h: 1,
      fill: p.tool === "text" ? "transparent" : p.color,
      stroke: p.tool === "text" ? "transparent" : "#1f2937",
    });
    p.onSelect(id);
    setDrag({ kind: "create", id, x0: x, y0: y });
  };

  const onMove = (e: PointerEvent<SVGSVGElement>) => {
    const { x, y } = point(e);
    const now = performance.now();
    if (now - lastCursor.current > 1000 / CURSOR_HZ) {
      lastCursor.current = now;
      p.awareness?.setLocalStateField("cursor", { x, y });
    }
    if (!drag) return;
    if (drag.kind === "move") {
      updateShape(p.doc, drag.id, { x: x - drag.dx, y: y - drag.dy });
    } else {
      updateShape(p.doc, drag.id, {
        x: Math.min(x, drag.x0), y: Math.min(y, drag.y0),
        w: Math.max(Math.abs(x - drag.x0), 8), h: Math.max(Math.abs(y - drag.y0), 8),
      });
    }
  };

  const onUp = () => {
    if (drag?.kind === "create") {
      const s = p.shapes.find((s) => s.id === drag.id);
      if (s?.type === "text") p.onEditText(drag.id);
      p.onToolDone();
    }
    setDrag(null);
  };

  const peerSelection = new Map(p.peers.filter((x) => x.selection).map((x) => [x.selection!, x.color]));

  return (
    <svg
      ref={svg}
      className="h-full w-full touch-none bg-white"
      onPointerDown={onDown}
      onPointerMove={onMove}
      onPointerUp={onUp}
      onPointerLeave={() => p.awareness?.setLocalStateField("cursor", null)}
      onDoubleClick={(e) => {
        const id = (e.target as SVGElement).dataset.shape;
        if (id && p.shapes.find((s) => s.id === id)?.type === "text") p.onEditText(id);
      }}
    >
      <defs>
        <pattern id="grid" width="24" height="24" patternUnits="userSpaceOnUse">
          <circle cx="1" cy="1" r="1" fill="#e5e7eb" />
        </pattern>
      </defs>
      <rect width="100%" height="100%" fill="url(#grid)" />

      {p.shapes.map((s) => {
        const ring = s.id === p.selected ? "#2563eb" : peerSelection.get(s.id);
        const common = { "data-shape": s.id, fill: s.fill, stroke: s.stroke, strokeWidth: 1.5, style: { cursor: "move" } };
        return (
          <g key={s.id}>
            {s.type === "rect" && <rect x={s.x} y={s.y} width={s.w} height={s.h} rx={6} {...common} />}
            {s.type === "ellipse" && (
              <ellipse cx={s.x + s.w / 2} cy={s.y + s.h / 2} rx={s.w / 2} ry={s.h / 2} {...common} />
            )}
            {s.type === "text" && (
              <text data-shape={s.id} x={s.x} y={s.y + 18} fontSize={18} fill="#111827" style={{ cursor: "move", userSelect: "none" }}>
                {s.text}
              </text>
            )}
            {ring && (
              <rect x={s.x - 4} y={s.y - 4} width={s.w + 8} height={s.h + 8} rx={8}
                fill="none" stroke={ring} strokeWidth={2} strokeDasharray={s.id === p.selected ? undefined : "4 3"} pointerEvents="none" />
            )}
          </g>
        );
      })}

      {p.peers.filter((x) => x.cursor).map((x) => (
        <g key={x.clientId} transform={`translate(${x.cursor!.x} ${x.cursor!.y})`} pointerEvents="none">
          <path d="M0 0 L0 16 L4.5 12 L8 19 L10.5 18 L7 11 L13 11 Z" fill={x.color} stroke="white" strokeWidth={1} />
          <rect x={14} y={14} rx={4} width={x.name.length * 7.5 + 12} height={20} fill={x.color} />
          <text x={20} y={28} fontSize={12} fill="white">{x.name}</text>
        </g>
      ))}
    </svg>
  );
}
