// Package hub owns rooms and the clients connected to them on this instance,
// and bridges each room to the other instances through a Bus.
//
// Concurrency model: each client has a buffered send channel drained by its
// own writer goroutine. Broadcasting never blocks on a slow client. If its
// buffer is full the client is evicted, and it reconnects and resyncs. One
// slow laptop on hotel Wi-Fi must not add latency for everyone else in the
// room.
package hub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/mbiskho/collaborative-canvas/server/internal/protocol"
)

// Store persists document updates. Implementations must be safe for
// concurrent use.
type Store interface {
	Append(ctx context.Context, room string, update []byte) error
	Load(ctx context.Context, room string) ([][]byte, error)
}

// Bus fans frames out to other server instances. Publish must not block for
// long; Subscribe delivers frames from other instances until ctx is done.
type Bus interface {
	Publish(ctx context.Context, room string, msg Envelope) error
	Subscribe(ctx context.Context, room string, deliver func(Envelope)) error
}

// Envelope is what travels over the Bus: the frame plus where it came from,
// so an instance can skip its own messages and the original sender.
type Envelope struct {
	Instance string
	Sender   uint64
	Frame    []byte
}

// Metrics is the subset of instrumentation the hub reports.
type Metrics interface {
	FrameIn(t protocol.Type)
	FrameOut(t protocol.Type, n int)
	Evicted()
	RoomsDelta(d int)
	ClientsDelta(d int)
}

// Client is one WebSocket connection. Send is drained by the connection's
// writer goroutine; Closed is closed when the hub evicts the client.
type Client struct {
	ID     uint64
	Send   chan []byte
	Closed chan struct{}
	once   sync.Once
}

func (c *Client) evict() { c.once.Do(func() { close(c.Closed) }) }

type room struct {
	id      string
	mu      sync.RWMutex
	clients map[*Client]struct{}
	cancel  context.CancelFunc // stops the bus subscription
}

type Hub struct {
	instance string
	store    Store
	bus      Bus
	metrics  Metrics
	log      *slog.Logger
	sendBuf  int

	mu    sync.Mutex
	rooms map[string]*room

	nextID atomic.Uint64
}

type Option func(*Hub)

func WithSendBuffer(n int) Option { return func(h *Hub) { h.sendBuf = n } }

func New(store Store, bus Bus, m Metrics, log *slog.Logger, opts ...Option) *Hub {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	h := &Hub{
		instance: hex.EncodeToString(b),
		store:    store,
		bus:      bus,
		metrics:  m,
		log:      log,
		sendBuf:  256,
		rooms:    make(map[string]*room),
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Instance identifies this server process on the bus.
func (h *Hub) Instance() string { return h.instance }

// Join registers a client in a room and streams the room's stored history to
// it, followed by a Synced frame. The client is live (receives broadcasts)
// before history is loaded, so no update can fall into the gap between
// "loaded" and "subscribed"; duplicates are harmless because Yjs updates are
// idempotent.
func (h *Hub) Join(ctx context.Context, roomID string) (*Client, error) {
	c := &Client{
		ID:     h.nextID.Add(1),
		Send:   make(chan []byte, h.sendBuf),
		Closed: make(chan struct{}),
	}
	r := h.room(roomID)
	r.mu.Lock()
	r.clients[c] = struct{}{}
	r.mu.Unlock()
	h.metrics.ClientsDelta(1)

	history, err := h.store.Load(ctx, roomID)
	if err != nil {
		h.Leave(roomID, c)
		return nil, err
	}
	for _, u := range history {
		if !h.offer(c, protocol.Encode(protocol.Update, u)) {
			return c, nil // evicted mid-sync; it will reconnect
		}
	}
	h.offer(c, protocol.Encode(protocol.Synced, nil))
	return c, nil
}

// Leave removes a client. The last client out closes the room and its bus
// subscription.
func (h *Hub) Leave(roomID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.rooms[roomID]
	if !ok {
		return
	}
	r.mu.Lock()
	if _, present := r.clients[c]; present {
		delete(r.clients, c)
		h.metrics.ClientsDelta(-1)
	}
	empty := len(r.clients) == 0
	r.mu.Unlock()
	c.evict()
	if empty {
		r.cancel()
		delete(h.rooms, roomID)
		h.metrics.RoomsDelta(-1)
	}
}

// Receive handles a frame from a local client: relay it to the room on this
// instance, publish it to the others, and persist it if it is document state.
func (h *Hub) Receive(ctx context.Context, roomID string, from *Client, frame []byte) error {
	t, payload, err := protocol.Parse(frame)
	if err != nil {
		return err
	}
	if t == protocol.Synced {
		return protocol.ErrUnknown // server-to-client only
	}
	h.metrics.FrameIn(t)

	// Fan-out first: it is the latency-critical path. Persistence is not on it.
	h.broadcast(roomID, frame, from.ID)

	if err := h.bus.Publish(ctx, roomID, Envelope{Instance: h.instance, Sender: from.ID, Frame: frame}); err != nil {
		h.log.Warn("bus publish failed", "room", roomID, "err", err)
	}
	if t.Persisted() {
		cp := append([]byte(nil), payload...)
		if err := h.store.Append(ctx, roomID, cp); err != nil {
			return err
		}
	}
	return nil
}

func (h *Hub) room(id string) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.rooms[id]; ok {
		return r
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &room{id: id, clients: make(map[*Client]struct{}), cancel: cancel}
	h.rooms[id] = r
	h.metrics.RoomsDelta(1)
	go func() {
		err := h.bus.Subscribe(ctx, id, func(e Envelope) {
			if e.Instance == h.instance {
				return // already delivered locally
			}
			h.broadcast(id, e.Frame, 0)
		})
		if err != nil && ctx.Err() == nil {
			h.log.Error("bus subscription ended", "room", id, "err", err)
		}
	}()
	return r
}

func (h *Hub) broadcast(roomID string, frame []byte, except uint64) {
	h.mu.Lock()
	r, ok := h.rooms[roomID]
	h.mu.Unlock()
	if !ok {
		return
	}
	t := protocol.Type(frame[0])
	r.mu.RLock()
	targets := make([]*Client, 0, len(r.clients))
	for c := range r.clients {
		if c.ID != except {
			targets = append(targets, c)
		}
	}
	r.mu.RUnlock()

	sent := 0
	for _, c := range targets {
		if h.offer(c, frame) {
			sent++
		} else {
			h.Leave(roomID, c)
		}
	}
	h.metrics.FrameOut(t, sent)
}

// offer is a non-blocking send. False means the client was too slow and has
// been evicted.
func (h *Hub) offer(c *Client, frame []byte) bool {
	select {
	case <-c.Closed:
		return false
	default:
	}
	select {
	case c.Send <- frame:
		return true
	default:
		h.metrics.Evicted()
		c.evict()
		return false
	}
}

// Stats is a point-in-time view for /debug and tests.
func (h *Hub) Stats() (rooms, clients int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.rooms {
		r.mu.RLock()
		clients += len(r.clients)
		r.mu.RUnlock()
	}
	return len(h.rooms), clients
}
