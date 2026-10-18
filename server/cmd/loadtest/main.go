// Command loadtest opens many clients across rooms, sends timestamped
// awareness frames, and measures send-to-receive latency at the peers.
//
//	go run ./cmd/loadtest -url ws://localhost:8080 -rooms 20 -clients 5 -rate 20 -duration 60s
//
// Awareness frames are used because the server relays them without storing
// them, so the benchmark doesn't fill the database with junk. They take the
// same fan-out path as document updates.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mbiskho/collaborative-canvas/server/internal/protocol"
)

const magic = 0xC4

type result struct {
	Rooms, ClientsPerRoom int
	RatePerClient         float64
	Duration              string
	Sent, Received        int64
	SentPerSec            float64 `json:"sent_per_sec"`
	DeliveredPerSec       float64 `json:"delivered_per_sec"`
	Errors                int64
	P50Ms, P95Ms, P99Ms   float64
	MaxMs                 float64
}

func main() {
	url := flag.String("url", "ws://localhost:8080", "server base URL")
	rooms := flag.Int("rooms", 20, "rooms")
	clients := flag.Int("clients", 5, "clients per room")
	rateHz := flag.Float64("rate", 20, "frames per second per client")
	dur := flag.Duration("duration", 30*time.Second, "test duration")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *dur)
	defer cancel()

	var sent, recv, errs atomic.Int64
	var mu sync.Mutex
	lat := make([]float64, 0, 1<<20)

	var wg sync.WaitGroup
	for r := 0; r < *rooms; r++ {
		room := fmt.Sprintf("bench-%03d", r)
		for c := 0; c < *clients; c++ {
			wg.Add(1)
			go func(id uint32) {
				defer wg.Done()
				conn, _, err := websocket.DefaultDialer.DialContext(ctx, *url+"/ws/"+room, nil)
				if err != nil {
					errs.Add(1)
					return
				}
				defer conn.Close()

				// reader
				go func() {
					for {
						_, f, err := conn.ReadMessage()
						if err != nil {
							return
						}
						t, p, err := protocol.Parse(f)
						if err != nil || t != protocol.Awareness || len(p) < 13 || p[0] != magic {
							continue
						}
						sentAt := int64(binary.BigEndian.Uint64(p[1:9]))
						ms := float64(time.Now().UnixNano()-sentAt) / 1e6
						recv.Add(1)
						mu.Lock()
						lat = append(lat, ms)
						mu.Unlock()
					}
				}()

				tick := time.NewTicker(time.Duration(float64(time.Second) / *rateHz))
				defer tick.Stop()
				payload := make([]byte, 13+48) // ~ size of a real cursor awareness update
				payload[0] = magic
				binary.BigEndian.PutUint32(payload[9:13], id)
				for {
					select {
					case <-ctx.Done():
						return
					case <-tick.C:
						binary.BigEndian.PutUint64(payload[1:9], uint64(time.Now().UnixNano()))
						if err := conn.WriteMessage(websocket.BinaryMessage, protocol.Encode(protocol.Awareness, payload)); err != nil {
							errs.Add(1)
							return
						}
						sent.Add(1)
					}
				}
			}(uint32(r*(*clients) + c))
		}
	}
	wg.Wait()

	sort.Float64s(lat)
	q := func(p float64) float64 {
		if len(lat) == 0 {
			return 0
		}
		return lat[int(p*float64(len(lat)-1))]
	}
	secs := dur.Seconds()
	res := result{
		Rooms: *rooms, ClientsPerRoom: *clients, RatePerClient: *rateHz, Duration: dur.String(),
		Sent: sent.Load(), Received: recv.Load(), Errors: errs.Load(),
		SentPerSec: float64(sent.Load()) / secs, DeliveredPerSec: float64(recv.Load()) / secs,
		P50Ms: q(0.50), P95Ms: q(0.95), P99Ms: q(0.99), MaxMs: q(1),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(res)
}
