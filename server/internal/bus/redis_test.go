package bus

import (
	"bytes"
	"testing"

	"github.com/mbiskho/collaborative-canvas/server/internal/hub"
	"github.com/mbiskho/collaborative-canvas/server/internal/protocol"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	in := hub.Envelope{Instance: "a1b2c3d4e5f60708", Sender: 42, Frame: protocol.Encode(protocol.Update, []byte{7, 8, 9})}
	out, err := Unmarshal(Marshal(in))
	if err != nil {
		t.Fatal(err)
	}
	if out.Instance != in.Instance || out.Sender != in.Sender || !bytes.Equal(out.Frame, in.Frame) {
		t.Fatalf("got %+v", out)
	}
}

func TestUnmarshalRejectsTruncated(t *testing.T) {
	b := Marshal(hub.Envelope{Instance: "x", Sender: 1, Frame: []byte{1}})
	for i := 0; i < len(b); i++ {
		if _, err := Unmarshal(b[:i]); err == nil {
			t.Fatalf("accepted %d-byte prefix", i)
		}
	}
}

func TestChannelsSplitPresenceFromState(t *testing.T) {
	if channel("r", protocol.Update) == channel("r", protocol.Awareness) {
		t.Fatal("updates and awareness must use separate channels")
	}
}
