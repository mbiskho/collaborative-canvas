// Package store persists Yjs updates in PostgreSQL.
//
// Writes are batched off the fan-out path: Append enqueues and returns, and a
// single writer flushes every FlushInterval or BatchSize updates, whichever
// comes first. Load reads the in-memory pending queue before the database, so
// a client that joins between "broadcast" and "flushed" still gets every
// update. It may get one twice, which Yjs ignores.
package store

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Observer interface {
	Flushed(batch int, took time.Duration)
}

type Postgres struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	obs  Observer

	BatchSize     int
	FlushInterval time.Duration

	queue chan pending

	mu      sync.Mutex
	inQueue map[string][][]byte // room -> updates enqueued but not yet committed

	done chan struct{}
}

type pending struct {
	room   string
	update []byte
}

func NewPostgres(ctx context.Context, dsn string, log *slog.Logger, obs Observer) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	p := &Postgres{
		pool:          pool,
		log:           log,
		obs:           obs,
		BatchSize:     256,
		FlushInterval: 20 * time.Millisecond,
		queue:         make(chan pending, 8192),
		inQueue:       make(map[string][][]byte),
		done:          make(chan struct{}),
	}
	go p.writer()
	return p, nil
}

// Append enqueues an update. It blocks only when the queue is full, which is
// deliberate backpressure: better to slow senders than drop document state.
func (p *Postgres) Append(ctx context.Context, room string, update []byte) error {
	p.mu.Lock()
	p.inQueue[room] = append(p.inQueue[room], update)
	p.mu.Unlock()
	select {
	case p.queue <- pending{room, update}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Load returns the room's snapshot (if the compactor has written one), the
// updates after it, and anything still waiting to be flushed.
func (p *Postgres) Load(ctx context.Context, room string) ([][]byte, error) {
	// Pending first, then the database: if a flush lands in between, we see
	// the update twice instead of not at all.
	p.mu.Lock()
	queued := append([][]byte(nil), p.inQueue[room]...)
	p.mu.Unlock()

	var out [][]byte
	var upto int64
	var snap []byte
	err := p.pool.QueryRow(ctx,
		`SELECT state, upto_id FROM doc_snapshots WHERE room = $1`, room).Scan(&snap, &upto)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		out = append(out, snap)
	}

	rows, err := p.pool.Query(ctx,
		`SELECT update FROM doc_updates WHERE room = $1 AND id > $2 ORDER BY id`, room, upto)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u []byte
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return append(out, queued...), nil
}

func (p *Postgres) writer() {
	defer close(p.done)
	ticker := time.NewTicker(p.FlushInterval)
	defer ticker.Stop()
	batch := make([]pending, 0, p.BatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		start := time.Now()
		rows := make([][]any, len(batch))
		for i, b := range batch {
			rows[i] = []any{b.room, b.update}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := p.pool.CopyFrom(ctx, pgx.Identifier{"doc_updates"}, []string{"room", "update"}, pgx.CopyFromRows(rows))
		cancel()
		if err != nil {
			// Keep them queued in memory; clients also resend full state on
			// reconnect, so a lost batch is recoverable, not fatal.
			p.log.Error("flush failed; will retry", "batch", len(batch), "err", err)
			return
		}
		p.mu.Lock()
		for _, b := range batch {
			q := p.inQueue[b.room]
			if len(q) > 0 {
				q = q[1:] // FIFO per room matches enqueue order
			}
			if len(q) == 0 {
				delete(p.inQueue, b.room)
			} else {
				p.inQueue[b.room] = q
			}
		}
		p.mu.Unlock()
		if p.obs != nil {
			p.obs.Flushed(len(batch), time.Since(start))
		}
		batch = batch[:0]
	}

	for {
		select {
		case item, ok := <-p.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, item)
			if len(batch) >= p.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// Close flushes what is queued and closes the pool.
func (p *Postgres) Close() {
	close(p.queue)
	<-p.done
	p.pool.Close()
}
