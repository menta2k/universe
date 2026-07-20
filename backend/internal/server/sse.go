package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/valkey-io/valkey-go"
)

// EventStreamer serves the SSE endpoint backed by Valkey pub/sub (FR-012,
// SC-004). Clients may filter by machine_id or session_id query params.
type EventStreamer struct {
	vk      valkey.Client
	channel string
	log     *slog.Logger
}

func NewEventStreamer(vk valkey.Client, channel string, log *slog.Logger) *EventStreamer {
	return &EventStreamer{vk: vk, channel: channel, log: log}
}

type streamEvent struct {
	SessionID  string `json:"session_id"`
	MachineMAC string `json:"machine_mac"`
}

// ServeHTTP streams events as text/event-stream until the client disconnects.
func (s *EventStreamer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Flush headers immediately so the client's request returns and can start
	// reading; otherwise Receive blocks before any bytes are sent and the
	// client stalls waiting for the response head.
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	filterSession := r.URL.Query().Get("session_id")
	filterMachine := r.URL.Query().Get("machine_id") // matched against MAC filter param
	filterMAC := r.URL.Query().Get("machine_mac")

	ctx := r.Context()
	// Dedicated client for the blocking subscribe.
	dedicated, cancel := s.vk.Dedicate()
	defer cancel()

	err := dedicated.Receive(ctx, dedicated.B().Subscribe().Channel(s.channel).Build(),
		func(msg valkey.PubSubMessage) {
			if !passesFilter(msg.Message, filterSession, filterMAC) {
				return
			}
			_, _ = w.Write([]byte("data: " + msg.Message + "\n\n"))
			flusher.Flush()
		})
	if err != nil && ctx.Err() == nil {
		s.log.Error("sse subscribe ended", "err", err)
	}
	_ = filterMachine
}

func passesFilter(payload, session, mac string) bool {
	if session == "" && mac == "" {
		return true
	}
	var e streamEvent
	if err := json.Unmarshal([]byte(payload), &e); err != nil {
		return true
	}
	if session != "" && e.SessionID != session {
		return false
	}
	if mac != "" && e.MachineMAC != mac {
		return false
	}
	return true
}

// RegisterSSE mounts the stream on the given mux path.
func RegisterSSE(mux interface {
	HandleFunc(string, func(http.ResponseWriter, *http.Request))
}, path string, s *EventStreamer) {
	mux.HandleFunc(path, s.ServeHTTP)
}
