package hub

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/mbiskho/collaborative-canvas/server/internal/protocol"
)

func newHub(t *testing.T, bus Bus, opts ...Option) (*Hub, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	if bus == nil {
		bus = LocalBus{}
	}
	return New(store, bus, NopMetrics{}, slog.New(slog.NewTextHandler(io.Discard, nil)), opts...), store
}

func drainUntilSynced(t *testing.T, c *Client) [][]byte {
	t.Helper()
	var got [][]byte
	for {
		select {
		case f := <-c.Send:
			if protocol.Type(f[0]) == protocol.Synced {
				return got
			}
			got = append(got, f)
		case <-time.After(time.Second):
			t.Fatal("no Synced frame")
		}
	}
}

func recv(t *testing.T, c *Client) []byte {
	t.Helper()
	select {
	case f := <-c.Send:
		return f
	case <-time.After(time.Second):
		t.Fatal("expected a frame")
		return nil
	}
}

func assertNothing(t *testing.T, c *Client) {
	t.Helper()
	select {
	case f := <-c.Send:
		t.Fatalf("unexpected frame %x", f)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestBroadcastSkipsSender(t *testing.T) {
	h, _ := newHub(t, nil)
	ctx := context.Background()
	a, _ := h.Join(ctx, "r")
	b, _ := h.Join(ctx, "r")
	drainUntilSynced(t, a)
	drainUntilSynced(t, b)

	frame := protocol.Encode(protocol.Update, []byte{1, 2, 3})
	if err := h.Receive(ctx, "r", a, frame); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, b); string(got) != string(frame) {
		t.Fatalf("b got %x", got)
	}
	assertNothing(t, a)
}

func TestRoomsAreIsolated(t *testing.T) {
	h, _ := newHub(t, nil)
	ctx := context.Background()
	a, _ := h.Join(ctx, "room-a")
	b, _ := h.Join(ctx, "room-b")
	drainUntilSynced(t, a)
	drainUntilSynced(t, b)

	_ = h.Receive(ctx, "room-a", a, protocol.Encode(protocol.Update, []byte{9}))
	assertNothing(t, b)
}

func TestOnlyUpdatesArePersistedAndReplayed(t *testing.T) {
	h, store := newHub(t, nil)
	ctx := context.Background()
	a, _ := h.Join(ctx, "r")
	drainUntilSynced(t, a)

	_ = h.Receive(ctx, "r", a, protocol.Encode(protocol.Update, []byte{1}))
	_ = h.Receive(ctx, "r", a, protocol.Encode(protocol.Awareness, []byte{2}))
	_ = h.Receive(ctx, "r", a, protocol.Encode(protocol.Update, []byte{3}))

	hist, _ := store.Load(ctx, "r")
	if len(hist) != 2 {
		t.Fatalf("stored %d updates, want 2 (awareness must not be stored)", len(hist))
	}

	// A late joiner receives exactly the stored updates, then Synced.
	late, _ := h.Join(ctx, "r")
	replayed := drainUntilSynced(t, late)
	if len(replayed) != 2 || replayed[0][1] != 1 || replayed[1][1] != 3 {
		t.Fatalf("replayed %x", replayed)
	}
}

func TestSlowClientIsEvictedNotWaitedOn(t *testing.T) {
	h, _ := newHub(t, nil, WithSendBuffer(4))
	ctx := context.Background()
	fast, _ := h.Join(ctx, "r")
	slow, _ := h.Join(ctx, "r")
	drainUntilSynced(t, fast)
	// slow never drains its channel.

	sender, _ := h.Join(ctx, "r")
	drainUntilSynced(t, sender)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			_ = h.Receive(ctx, "r", sender, protocol.Encode(protocol.Awareness, []byte{byte(i)}))
			<-fast.Send
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("broadcast blocked on a slow client")
	}
	select {
	case <-slow.Closed:
	default:
		t.Fatal("slow client was not evicted")
	}
	if _, clients := h.Stats(); clients != 2 {
		t.Fatalf("clients = %d, want 2", clients)
	}
}

func TestLastClientClosesRoom(t *testing.T) {
	h, _ := newHub(t, nil)
	ctx := context.Background()
	a, _ := h.Join(ctx, "r")
	b, _ := h.Join(ctx, "r")
	h.Leave("r", a)
	if rooms, _ := h.Stats(); rooms != 1 {
		t.Fatalf("rooms = %d, want 1", rooms)
	}
	h.Leave("r", b)
	if rooms, clients := h.Stats(); rooms != 0 || clients != 0 {
		t.Fatalf("rooms, clients = %d, %d; want 0, 0", rooms, clients)
	}
}

// chanBus connects hubs in one process, standing in for Redis.
type chanBus struct {
	mu   sync.Mutex
	subs map[string][]func(Envelope)
}

func (b *chanBus) Publish(_ context.Context, room string, e Envelope) error {
	b.mu.Lock()
	subs := append([]func(Envelope){}, b.subs[room]...)
	b.mu.Unlock()
	for _, s := range subs {
		s(e)
	}
	return nil
}

func (b *chanBus) Subscribe(ctx context.Context, room string, deliver func(Envelope)) error {
	b.mu.Lock()
	if b.subs == nil {
		b.subs = map[string][]func(Envelope){}
	}
	b.subs[room] = append(b.subs[room], deliver)
	b.mu.Unlock()
	<-ctx.Done()
	return nil
}

func TestCrossInstanceFanout(t *testing.T) {
	bus := &chanBus{}
	h1, _ := newHub(t, bus)
	h2, _ := newHub(t, bus)
	ctx := context.Background()

	a, _ := h1.Join(ctx, "r")
	b, _ := h2.Join(ctx, "r")
	drainUntilSynced(t, a)
	drainUntilSynced(t, b)
	time.Sleep(20 * time.Millisecond) // let both subscriptions register

	frame := protocol.Encode(protocol.Awareness, []byte("cursor"))
	_ = h1.Receive(ctx, "r", a, frame)

	if got := recv(t, b); string(got) != string(frame) {
		t.Fatalf("instance 2 got %x", got)
	}
	assertNothing(t, a) // h1 ignores its own echo from the bus
}
