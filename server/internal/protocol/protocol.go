// Package protocol defines the wire format between browser and server.
//
// Every WebSocket binary message is one frame: a 1-byte type followed by an
// opaque payload. The server never decodes Yjs payloads. Yjs updates are
// commutative and idempotent, so the server only has to store and relay them,
// never merge them. That keeps the Go side small and the CRDT logic in one
// place, the client.
package protocol

import "errors"

// Type is the first byte of every frame.
type Type byte

const (
	// Update carries a Yjs document update (Y.encodeStateAsUpdate or an
	// incremental update). Persisted and fanned out.
	Update Type = 0x01
	// Awareness carries a y-protocols awareness update: cursors, selection,
	// user colour. Fanned out, never persisted.
	Awareness Type = 0x02
	// Synced is sent by the server after the stored history of a room has been
	// streamed to a joining client. Payload is empty.
	Synced Type = 0x03
)

// MaxFrame bounds a single frame. A full-state resend of a large canvas is the
// biggest legitimate frame, so this is generous; anything bigger is abuse.
const MaxFrame = 2 << 20 // 2 MiB

var (
	ErrEmpty    = errors.New("protocol: empty frame")
	ErrTooLarge = errors.New("protocol: frame exceeds MaxFrame")
	ErrUnknown  = errors.New("protocol: unknown frame type")
)

// Parse validates a frame and returns its type and payload. The payload
// aliases the input; callers that keep it must copy.
func Parse(frame []byte) (Type, []byte, error) {
	if len(frame) == 0 {
		return 0, nil, ErrEmpty
	}
	if len(frame) > MaxFrame {
		return 0, nil, ErrTooLarge
	}
	t := Type(frame[0])
	switch t {
	case Update, Awareness, Synced:
		return t, frame[1:], nil
	default:
		return 0, nil, ErrUnknown
	}
}

// Encode builds a frame from a type and payload.
func Encode(t Type, payload []byte) []byte {
	out := make([]byte, 1+len(payload))
	out[0] = byte(t)
	copy(out[1:], payload)
	return out
}

// Persisted reports whether frames of this type belong in the room's history.
// This is the line between durable CRDT state and ephemeral presence: cursor
// moves are most of the traffic and none of the state.
func (t Type) Persisted() bool { return t == Update }

func (t Type) String() string {
	switch t {
	case Update:
		return "update"
	case Awareness:
		return "awareness"
	case Synced:
		return "synced"
	default:
		return "unknown"
	}
}
