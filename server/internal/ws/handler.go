// Package ws adapts WebSocket connections to the hub.
package ws

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/mbiskho/collaborative-canvas/server/internal/hub"
	"github.com/mbiskho/collaborative-canvas/server/internal/protocol"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 45 * time.Second
	pingPeriod = 20 * time.Second

	// Awareness is throttled per client on the server too. A buggy or
	// malicious client sending cursor updates at 1 kHz would otherwise be
	// multiplied by every peer in the room and every instance.
	awarenessPerSecond = 30
	awarenessBurst     = 10
)

var roomID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Timer interface{ Handled(time.Duration) }

type Handler struct {
	Hub      *hub.Hub
	Log      *slog.Logger
	Timer    Timer
	upgrader websocket.Upgrader
}

func New(h *hub.Hub, log *slog.Logger, t Timer, allowedOrigins []string) *Handler {
	allowed := map[string]bool{}
	for _, o := range allowedOrigins {
		allowed[o] = true
	}
	return &Handler{
		Hub: h, Log: log, Timer: t,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				return len(allowed) == 0 || allowed[r.Header.Get("Origin")]
			},
		},
	}
}

func (h *Handler) Serve(c *gin.Context) {
	room := c.Param("room")
	if !roomID.MatchString(room) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // the upgrader already wrote the HTTP error
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	client, err := h.Hub.Join(ctx, room)
	if err != nil {
		h.Log.Error("join failed", "room", room, "err", err)
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "load failed"), time.Now().Add(writeWait))
		return
	}
	defer h.Hub.Leave(room, client)

	go h.writePump(conn, client, cancel)
	h.readPump(ctx, conn, room, client)
}

func (h *Handler) readPump(ctx context.Context, conn *websocket.Conn, room string, client *hub.Client) {
	conn.SetReadLimit(protocol.MaxFrame)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongWait)) })
	limiter := rate.NewLimiter(awarenessPerSecond, awarenessBurst)

	for {
		kind, frame, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if kind != websocket.BinaryMessage || len(frame) == 0 {
			continue
		}
		if protocol.Type(frame[0]) == protocol.Awareness && !limiter.Allow() {
			continue // drop, don't disconnect: the next cursor position supersedes this one
		}
		start := time.Now()
		if err := h.Hub.Receive(ctx, room, client, frame); err != nil {
			h.Log.Warn("bad frame", "room", room, "client", client.ID, "err", err)
			return
		}
		if h.Timer != nil {
			h.Timer.Handled(time.Since(start))
		}
	}
}

func (h *Handler) writePump(conn *websocket.Conn, client *hub.Client, cancel context.CancelFunc) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		cancel()
		conn.Close() // unblocks readPump
	}()
	for {
		select {
		case frame := <-client.Send:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				return
			}
		case <-client.Closed:
			_ = conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "evicted: too slow"), time.Now().Add(writeWait))
			return
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				return
			}
		}
	}
}
