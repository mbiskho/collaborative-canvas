package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	for _, typ := range []Type{Update, Awareness, Synced} {
		payload := []byte{0xde, 0xad, 0xbe, 0xef}
		got, p, err := Parse(Encode(typ, payload))
		if err != nil {
			t.Fatalf("%v: %v", typ, err)
		}
		if got != typ || !bytes.Equal(p, payload) {
			t.Fatalf("%v: got (%v, %x)", typ, got, p)
		}
	}
}

func TestRejects(t *testing.T) {
	cases := map[string]struct {
		frame []byte
		want  error
	}{
		"empty":   {nil, ErrEmpty},
		"unknown": {[]byte{0x7f, 1}, ErrUnknown},
		"large":   {make([]byte, MaxFrame+1), ErrTooLarge},
	}
	for name, c := range cases {
		if _, _, err := Parse(c.frame); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}

func TestOnlyUpdatesArePersisted(t *testing.T) {
	if !Update.Persisted() || Awareness.Persisted() || Synced.Persisted() {
		t.Fatal("only Update frames may be persisted")
	}
}
