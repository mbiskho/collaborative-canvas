// Mirrors server/internal/protocol: one type byte, then an opaque payload.

export const FrameType = {
  Update: 0x01, // Yjs update: persisted + fanned out
  Awareness: 0x02, // y-protocols awareness: fanned out only
  Synced: 0x03, // server -> client: history fully streamed
} as const;

export type FrameType = (typeof FrameType)[keyof typeof FrameType];

export function encodeFrame(type: FrameType, payload: Uint8Array): Uint8Array {
  const out = new Uint8Array(payload.length + 1);
  out[0] = type;
  out.set(payload, 1);
  return out;
}

export function parseFrame(data: ArrayBuffer): { type: number; payload: Uint8Array } | null {
  const bytes = new Uint8Array(data);
  if (bytes.length === 0) return null;
  return { type: bytes[0], payload: bytes.subarray(1) };
}
