package integration

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/data"
	"github.com/menta2k/universe/backend/internal/server"
	"github.com/menta2k/universe/backend/tests/integration/testenv"
)

// TestSSEDeliversFilteredEvents verifies the SSE endpoint streams published
// events matching the session filter within a few seconds (SC-004).
func TestSSEDeliversFilteredEvents(t *testing.T) {
	env := testenv.Start(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	streamer := server.NewEventStreamer(env.Data.Valkey, data.EventsChannel, log)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/events/stream", streamer.ServeHTTP)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	events := biz.NewEventRecorder(data.NewEventRepo(env.Data), data.NewEventPublisher(env.Data), log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		ts.URL+"/api/v1/events/stream?session_id=sess-123", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect sse: %v", err)
	}
	defer resp.Body.Close()

	// Give the subscriber a moment to attach, then publish two events: one that
	// should be filtered out, one that should pass.
	time.Sleep(300 * time.Millisecond)
	events.Record(context.Background(), biz.Event{
		SessionID: "other", MachineMAC: "52:54:00:00:00:99",
		Phase: biz.PhaseIPXEScript, Outcome: biz.OutcomeOK})
	events.Record(context.Background(), biz.Event{
		SessionID: "sess-123", MachineMAC: "52:54:00:00:00:01",
		Phase: biz.PhaseSeedServed, Outcome: biz.OutcomeOK})

	line := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "data: ") {
				line <- scanner.Text()
				return
			}
		}
	}()

	select {
	case got := <-line:
		if !strings.Contains(got, "sess-123") || !strings.Contains(got, "seed_served") {
			t.Errorf("unexpected event delivered: %s", got)
		}
		if strings.Contains(got, "\"session_id\":\"other\"") {
			t.Error("filtered-out event leaked to client")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no SSE event within 5s (SC-004)")
	}
}
