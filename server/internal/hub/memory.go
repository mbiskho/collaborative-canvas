package hub

import (
	"context"
	"sync"

	"github.com/mbiskho/collaborative-canvas/server/internal/protocol"
)

// MemoryStore keeps history in process. Used by tests and `-store=memory`.
type MemoryStore struct {
	mu   sync.Mutex
	docs map[string][][]byte
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{docs: make(map[string][][]byte)} }

func (s *MemoryStore) Append(_ context.Context, room string, u []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs[room] = append(s.docs[room], u)
	return nil
}

func (s *MemoryStore) Load(_ context.Context, room string) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.docs[room]...), nil
}

// LocalBus is a single-process bus: with one instance there is nobody to tell.
type LocalBus struct{}

func (LocalBus) Publish(context.Context, string, Envelope) error { return nil }

func (LocalBus) Subscribe(ctx context.Context, _ string, _ func(Envelope)) error {
	<-ctx.Done()
	return nil
}

// NopMetrics discards instrumentation.
type NopMetrics struct{}

func (NopMetrics) FrameIn(protocol.Type)       {}
func (NopMetrics) FrameOut(protocol.Type, int) {}
func (NopMetrics) Evicted()                    {}
func (NopMetrics) RoomsDelta(int)              {}
func (NopMetrics) ClientsDelta(int)            {}
