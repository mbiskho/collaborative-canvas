import * as Y from "yjs";
import { nanoid } from "nanoid";

/**
 * Document layout:
 *
 *   doc.getMap("shapes"): shapeId -> Y.Map {
 *     type: "rect" | "ellipse" | "text"
 *     x, y, w, h: number
 *     fill, stroke: string
 *     z: number            fractional z-index; concurrent "bring to front" never collides
 *     text?: Y.Text        only for text shapes; character-level merge
 *   }
 *
 * Each property is its own map key, so two people moving and recolouring the
 * same shape both win (last-writer-wins per property, not per shape). Text is
 * a Y.Text so concurrent typing interleaves instead of overwriting.
 */

export type ShapeType = "rect" | "ellipse" | "text";

export interface Shape {
  id: string;
  type: ShapeType;
  x: number;
  y: number;
  w: number;
  h: number;
  fill: string;
  stroke: string;
  z: number;
  text?: string;
}

export const LOCAL = "local"; // transaction origin tracked by the undo manager

export function shapesMap(doc: Y.Doc): Y.Map<Y.Map<unknown>> {
  return doc.getMap("shapes");
}

export function addShape(doc: Y.Doc, init: Omit<Shape, "id" | "z"> & { z?: number }): string {
  const id = nanoid(10);
  doc.transact(() => {
    const m = new Y.Map<unknown>();
    const z = init.z ?? topZ(doc) + 1;
    for (const [k, v] of Object.entries({ ...init, z })) {
      if (k === "text") continue;
      m.set(k, v);
    }
    if (init.type === "text") {
      const t = new Y.Text();
      t.insert(0, init.text ?? "Text");
      m.set("text", t);
    }
    shapesMap(doc).set(id, m);
  }, LOCAL);
  return id;
}

export function updateShape(
  doc: Y.Doc,
  id: string,
  patch: Partial<Omit<Shape, "id" | "type" | "text">>,
): void {
  const m = shapesMap(doc).get(id);
  if (!m) return; // deleted concurrently by someone else: drop the edit
  doc.transact(() => {
    for (const [k, v] of Object.entries(patch)) m.set(k, v);
  }, LOCAL);
}

export function deleteShape(doc: Y.Doc, id: string): void {
  doc.transact(() => shapesMap(doc).delete(id), LOCAL);
}

/** Replace a text shape's content with the minimal insert/delete. */
export function setText(doc: Y.Doc, id: string, next: string): void {
  const t = shapesMap(doc).get(id)?.get("text");
  if (!(t instanceof Y.Text)) return;
  const prev = t.toString();
  let start = 0;
  while (start < prev.length && start < next.length && prev[start] === next[start]) start++;
  let endPrev = prev.length;
  let endNext = next.length;
  while (endPrev > start && endNext > start && prev[endPrev - 1] === next[endNext - 1]) {
    endPrev--;
    endNext--;
  }
  doc.transact(() => {
    if (endPrev > start) t.delete(start, endPrev - start);
    if (endNext > start) t.insert(start, next.slice(start, endNext));
  }, LOCAL);
}

export function bringToFront(doc: Y.Doc, id: string): void {
  updateShape(doc, id, { z: topZ(doc) + 1 });
}

export function topZ(doc: Y.Doc): number {
  let max = 0;
  shapesMap(doc).forEach((m) => {
    max = Math.max(max, Number(m.get("z") ?? 0));
  });
  return max;
}

export function readShapes(doc: Y.Doc): Shape[] {
  const out: Shape[] = [];
  shapesMap(doc).forEach((m, id) => {
    const text = m.get("text");
    out.push({
      id,
      type: m.get("type") as ShapeType,
      x: Number(m.get("x")),
      y: Number(m.get("y")),
      w: Number(m.get("w")),
      h: Number(m.get("h")),
      fill: String(m.get("fill")),
      stroke: String(m.get("stroke")),
      z: Number(m.get("z")),
      text: text instanceof Y.Text ? text.toString() : undefined,
    });
  });
  // Tie-break on id so every peer renders the same order.
  return out.sort((a, b) => a.z - b.z || a.id.localeCompare(b.id));
}
