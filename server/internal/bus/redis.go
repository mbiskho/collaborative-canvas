// Package bus fans frames out across server instances with Redis Pub/Sub.
//
// Each room uses two channels. Document updates go on canvas:{room}:doc and
// awareness (cursors) on canvas:{room}:aw. Cursor moves are around 90% of
// messages and none of the state, so keeping them separate lets ops see,
// throttle or shard them without touching the durable path.
package bus

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/mbiskho/collaborative-canvas/server/internal/hub"
	"github.com/mbiskho/collaborative-canvas/server/internal/protocol"
)

type Redis struct {
	rdb *redis.Client
}

func NewRedis(ctx context.Context, url string) (*Redis, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &Redis{rdb: rdb}, nil
}

func channel(room string, t protocol.Type) string {
	if t == protocol.Awareness {
		return fmt.Sprintf("canvas:%s:aw", room)
	}
	return fmt.Sprintf("canvas:%s:doc", room)
}

func (r *Redis) Publish(ctx context.Context, room string, e hub.Envelope) error {
	if len(e.Frame) == 0 {
		return nil
	}
	return r.rdb.Publish(ctx, channel(room, protocol.Type(e.Frame[0])), Marshal(e)).Err()
}

func (r *Redis) Subscribe(ctx context.Context, room string, deliver func(hub.Envelope)) error {
	ps := r.rdb.Subscribe(ctx, channel(room, protocol.Update), channel(room, protocol.Awareness))
	defer ps.Close()
	if _, err := ps.Receive(ctx); err != nil { // wait for the subscription to be confirmed
		return err
	}
	ch := ps.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return errors.New("redis pubsub channel closed")
			}
			e, err := Unmarshal([]byte(msg.Payload))
			if err != nil {
				continue
			}
			deliver(e)
		}
	}
}

func (r *Redis) Close() error { return r.rdb.Close() }

// Wire format: [1 byte instance length][instance][8 byte sender][frame].
func Marshal(e hub.Envelope) []byte {
	out := make([]byte, 0, 1+len(e.Instance)+8+len(e.Frame))
	out = append(out, byte(len(e.Instance)))
	out = append(out, e.Instance...)
	out = binary.BigEndian.AppendUint64(out, e.Sender)
	return append(out, e.Frame...)
}

func Unmarshal(b []byte) (hub.Envelope, error) {
	if len(b) < 1 {
		return hub.Envelope{}, errors.New("bus: short message")
	}
	n := int(b[0])
	if len(b) < 1+n+8+1 {
		return hub.Envelope{}, errors.New("bus: short message")
	}
	return hub.Envelope{
		Instance: string(b[1 : 1+n]),
		Sender:   binary.BigEndian.Uint64(b[1+n : 1+n+8]),
		Frame:    b[1+n+8:],
	}, nil
}
