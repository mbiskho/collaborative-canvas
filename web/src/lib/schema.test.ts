import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import { addShape, deleteShape, readShapes, setText, updateShape } from "./schema";

// Two docs that only talk by exchanging updates, like two browsers.
function pair() {
  const a = new Y.Doc();
  const b = new Y.Doc();
  const sync = () => {
    Y.applyUpdate(b, Y.encodeStateAsUpdate(a, Y.encodeStateVector(b)));
    Y.applyUpdate(a, Y.encodeStateAsUpdate(b, Y.encodeStateVector(a)));
  };
  return { a, b, sync };
}

const rect = { type: "rect" as const, x: 0, y: 0, w: 10, h: 10, fill: "#fff", stroke: "#000" };

describe("concurrent edits converge", () => {
  it("move and recolour of the same shape both survive", () => {
    const { a, b, sync } = pair();
    const id = addShape(a, rect);
    sync();
    updateShape(a, id, { x: 100, y: 50 }); // offline on A
    updateShape(b, id, { fill: "#f00" }); // offline on B
    sync();
    expect(readShapes(a)).toEqual(readShapes(b));
    expect(readShapes(a)[0]).toMatchObject({ x: 100, y: 50, fill: "#f00" });
  });

  it("concurrent typing in one text shape interleaves, nothing is lost", () => {
    const { a, b, sync } = pair();
    const id = addShape(a, { ...rect, type: "text", text: "hello world" });
    sync();
    setText(a, id, "hello brave world");
    setText(b, id, "hello world!");
    sync();
    expect(readShapes(a)[0].text).toBe("hello brave world!");
    expect(readShapes(b)[0].text).toBe("hello brave world!");
  });

  it("edit racing a delete does not resurrect the shape", () => {
    const { a, b, sync } = pair();
    const id = addShape(a, rect);
    sync();
    deleteShape(a, id);
    updateShape(b, id, { x: 5 });
    sync();
    expect(readShapes(a)).toHaveLength(0);
    expect(readShapes(b)).toHaveLength(0);
  });

  it("full-state resend after a drop is idempotent", () => {
    const { a, b, sync } = pair();
    addShape(a, rect);
    addShape(a, rect);
    sync();
    const before = readShapes(b);
    // What the provider does on every reconnect: send everything again.
    Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
    Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
    expect(readShapes(b)).toEqual(before);
  });

  it("renders in the same order on every peer", () => {
    const { a, b, sync } = pair();
    addShape(a, { ...rect, z: 1 });
    addShape(b, { ...rect, z: 1 }); // same z, concurrently
    sync();
    expect(readShapes(a).map((s) => s.id)).toEqual(readShapes(b).map((s) => s.id));
  });
});
