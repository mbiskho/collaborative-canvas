// Command server is the collaborative canvas relay: WebSocket rooms, Redis
// fan-out across instances, Postgres history.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mbiskho/collaborative-canvas/server/internal/bus"
	"github.com/mbiskho/collaborative-canvas/server/internal/hub"
	"github.com/mbiskho/collaborative-canvas/server/internal/metrics"
	"github.com/mbiskho/collaborative-canvas/server/internal/store"
	"github.com/mbiskho/collaborative-canvas/server/internal/ws"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := metrics.New(reg)

	// Storage: Postgres when DATABASE_URL is set (local Postgres or Supabase),
	// otherwise in memory for a zero-dependency `go run`.
	var st hub.Store = hub.NewMemoryStore()
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		pg, err := store.NewPostgres(ctx, dsn, log, m)
		if err != nil {
			log.Error("postgres", "err", err)
			os.Exit(1)
		}
		defer pg.Close()
		st = pg
	}

	// Fan-out: Redis when REDIS_URL is set, otherwise single-instance.
	var b hub.Bus = hub.LocalBus{}
	if url := os.Getenv("REDIS_URL"); url != "" {
		r, err := bus.NewRedis(ctx, url)
		if err != nil {
			log.Error("redis", "err", err)
			os.Exit(1)
		}
		defer r.Close()
		b = r
	}

	h := hub.New(st, b, m, log)
	var origins []string
	if o := os.Getenv("ALLOWED_ORIGINS"); o != "" {
		origins = strings.Split(o, ",")
	}
	handler := ws.New(h, log, m, origins)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/ws/:room", handler.Serve)
	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/metrics", gin.WrapH(promhttp.HandlerFor(reg, promhttp.HandlerOpts{})))
	r.GET("/debug/stats", func(c *gin.Context) {
		rooms, clients := h.Stats()
		c.JSON(http.StatusOK, gin.H{"instance": h.Instance(), "rooms": rooms, "clients": clients})
	})

	addr := envOr("ADDR", ":8080")
	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("listening", "addr", addr, "instance", h.Instance(),
			"store", storeName(st), "bus", busName(b))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown) // hijacked WebSockets close when their read fails
	log.Info("stopped")
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func storeName(s hub.Store) string {
	if _, ok := s.(*store.Postgres); ok {
		return "postgres"
	}
	return "memory"
}

func busName(b hub.Bus) string {
	if _, ok := b.(*bus.Redis); ok {
		return "redis"
	}
	return "local"
}
