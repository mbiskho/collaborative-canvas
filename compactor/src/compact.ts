// Folds each room's update log into one snapshot.
//
// Why a separate Node process: merging Yjs updates needs the Yjs library, and
// the relay server deliberately doesn't link one (it stays a dumb, fast
// pipe). Without compaction, a room's history grows with every keystroke and
// every reconnect's full-state resend, and join time grows with it.
//
//   DATABASE_URL=postgres://... node dist/compact.js            # once
//   DATABASE_URL=postgres://... INTERVAL_SECONDS=60 node dist/compact.js   # loop

import pg from "pg";
import * as Y from "yjs";

const MIN_UPDATES = Number(process.env.MIN_UPDATES ?? 200); // don't bother below this
const INTERVAL = Number(process.env.INTERVAL_SECONDS ?? 0);

interface Result {
  room: string;
  folded: number;
  bytesBefore: number;
  bytesAfter: number;
}

export async function compactRoom(client: pg.PoolClient, room: string): Promise<Result | null> {
  await client.query("BEGIN");
  try {
    // Lock the snapshot row (or the room's first update) so two compactors
    // can't fold the same range.
    const snap = await client.query<{ state: Buffer; upto_id: string }>(
      "SELECT state, upto_id FROM doc_snapshots WHERE room = $1 FOR UPDATE",
      [room],
    );
    const upto = snap.rows[0] ? BigInt(snap.rows[0].upto_id) : 0n;
    const rows = await client.query<{ id: string; update: Buffer }>(
      "SELECT id, update FROM doc_updates WHERE room = $1 AND id > $2 ORDER BY id FOR UPDATE SKIP LOCKED",
      [room, upto.toString()],
    );
    if (rows.rowCount === null || rows.rowCount < MIN_UPDATES) {
      await client.query("ROLLBACK");
      return null;
    }

    const parts: Uint8Array[] = [];
    if (snap.rows[0]) parts.push(snap.rows[0].state);
    for (const r of rows.rows) parts.push(r.update);
    const bytesBefore = parts.reduce((n, p) => n + p.byteLength, 0);

    // Load into a doc and re-encode rather than Y.mergeUpdates: re-encoding
    // also drops deleted content's payload (garbage collection), which is
    // where most of the size goes on a canvas people drag things around on.
    const doc = new Y.Doc();
    for (const p of parts) Y.applyUpdate(doc, p);
    const merged = Y.encodeStateAsUpdate(doc);
    doc.destroy();

    const maxId = rows.rows[rows.rows.length - 1].id;
    await client.query(
      `INSERT INTO doc_snapshots (room, state, upto_id, updated_at) VALUES ($1, $2, $3, now())
       ON CONFLICT (room) DO UPDATE SET state = EXCLUDED.state, upto_id = EXCLUDED.upto_id, updated_at = now()`,
      [room, Buffer.from(merged), maxId],
    );
    await client.query("DELETE FROM doc_updates WHERE room = $1 AND id <= $2", [room, maxId]);
    await client.query("COMMIT");
    return { room, folded: rows.rows.length, bytesBefore, bytesAfter: merged.byteLength };
  } catch (err) {
    await client.query("ROLLBACK");
    throw err;
  }
}

async function runOnce(pool: pg.Pool): Promise<void> {
  const rooms = await pool.query<{ room: string }>(
    "SELECT room FROM doc_updates GROUP BY room HAVING count(*) >= $1",
    [MIN_UPDATES],
  );
  for (const { room } of rooms.rows) {
    const client = await pool.connect();
    try {
      const r = await compactRoom(client, room);
      if (r) {
        console.log(JSON.stringify({
          msg: "compacted", ...r,
          ratio: +(r.bytesAfter / Math.max(r.bytesBefore, 1)).toFixed(3),
        }));
      }
    } catch (err) {
      console.error(JSON.stringify({ msg: "compaction failed", room, err: String(err) }));
    } finally {
      client.release();
    }
  }
}

async function main() {
  const pool = new pg.Pool({ connectionString: process.env.DATABASE_URL, max: 4 });
  do {
    await runOnce(pool);
    if (INTERVAL > 0) await new Promise((r) => setTimeout(r, INTERVAL * 1000));
  } while (INTERVAL > 0);
  await pool.end();
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
