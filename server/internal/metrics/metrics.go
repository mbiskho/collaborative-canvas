package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mbiskho/collaborative-canvas/server/internal/protocol"
)

type Prom struct {
	framesIn  *prometheus.CounterVec
	framesOut *prometheus.CounterVec
	evictions prometheus.Counter
	rooms     prometheus.Gauge
	clients   prometheus.Gauge
	flushSize prometheus.Histogram
	flushTime prometheus.Histogram
	handle    prometheus.Histogram
}

func New(reg prometheus.Registerer) *Prom {
	p := &Prom{
		framesIn: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "canvas_frames_in_total", Help: "Frames received from clients.",
		}, []string{"type"}),
		framesOut: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "canvas_frames_out_total", Help: "Frames delivered to client send buffers.",
		}, []string{"type"}),
		evictions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "canvas_slow_client_evictions_total", Help: "Clients dropped for a full send buffer.",
		}),
		rooms:   prometheus.NewGauge(prometheus.GaugeOpts{Name: "canvas_rooms", Help: "Open rooms on this instance."}),
		clients: prometheus.NewGauge(prometheus.GaugeOpts{Name: "canvas_clients", Help: "Connected clients on this instance."}),
		flushSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "canvas_store_flush_batch_size", Help: "Updates per Postgres COPY.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 10),
		}),
		flushTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "canvas_store_flush_seconds", Help: "Postgres COPY duration.",
			Buckets: prometheus.ExponentialBuckets(0.0005, 2, 12),
		}),
		handle: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "canvas_frame_handle_seconds", Help: "Time from frame read to fan-out complete.",
			Buckets: prometheus.ExponentialBuckets(0.00001, 2, 16),
		}),
	}
	reg.MustRegister(p.framesIn, p.framesOut, p.evictions, p.rooms, p.clients, p.flushSize, p.flushTime, p.handle)
	return p
}

func (p *Prom) FrameIn(t protocol.Type) { p.framesIn.WithLabelValues(t.String()).Inc() }
func (p *Prom) FrameOut(t protocol.Type, n int) {
	p.framesOut.WithLabelValues(t.String()).Add(float64(n))
}
func (p *Prom) Evicted()                { p.evictions.Inc() }
func (p *Prom) RoomsDelta(d int)        { p.rooms.Add(float64(d)) }
func (p *Prom) ClientsDelta(d int)      { p.clients.Add(float64(d)) }
func (p *Prom) Handled(d time.Duration) { p.handle.Observe(d.Seconds()) }
func (p *Prom) Flushed(batch int, took time.Duration) {
	p.flushSize.Observe(float64(batch))
	p.flushTime.Observe(took.Seconds())
}
