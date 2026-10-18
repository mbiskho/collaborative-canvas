-- Append-only update log. The relay server writes here (batched COPY);
-- the compactor folds old rows into doc_snapshots and deletes them.
CREATE TABLE IF NOT EXISTS doc_updates (
    id         BIGSERIAL PRIMARY KEY,
    room       TEXT        NOT NULL,
    update     BYTEA       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS doc_updates_room_id ON doc_updates (room, id);

-- One merged Yjs state per room. upto_id = the last doc_updates.id folded in;
-- the server loads the snapshot plus every update with id > upto_id.
CREATE TABLE IF NOT EXISTS doc_snapshots (
    room       TEXT        PRIMARY KEY,
    state      BYTEA       NOT NULL,
    upto_id    BIGINT      NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
