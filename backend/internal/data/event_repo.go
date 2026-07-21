package data

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/menta2k/universe/backend/internal/biz"
)

// EventsChannel is the Valkey pub/sub channel carrying live events for SSE.
const EventsChannel = "events"

// EventRepo writes provisioning events to the hypertable.
type EventRepo struct {
	data *Data
}

func NewEventRepo(d *Data) *EventRepo { return &EventRepo{data: d} }

func (r *EventRepo) Store(ctx context.Context, e biz.Event) error {
	detail, err := json.Marshal(e.Detail)
	if err != nil {
		return fmt.Errorf("marshal event detail: %w", err)
	}
	_, err = r.data.Pool.Exec(ctx,
		`INSERT INTO provisioning_events (time, session_id, machine_mac, phase, outcome, detail)
		 VALUES ($1, NULLIF($2,'')::uuid, NULLIF($3,'')::macaddr, $4, $5, $6)`,
		e.Time, e.SessionID, e.MachineMAC, string(e.Phase), string(e.Outcome), detail)
	if err != nil {
		return fmt.Errorf("insert provisioning event: %w", err)
	}
	return nil
}

// EventPublisher publishes events on the Valkey events channel.
type EventPublisher struct {
	data *Data
}

func NewEventPublisher(d *Data) *EventPublisher { return &EventPublisher{data: d} }

// wireEvent is the JSON shape consumed by the SSE endpoint and the frontend.
type wireEvent struct {
	Time       string         `json:"time"`
	SessionID  string         `json:"session_id,omitempty"`
	MachineMAC string         `json:"machine_mac,omitempty"`
	Phase      string         `json:"phase"`
	Outcome    string         `json:"outcome"`
	Detail     map[string]any `json:"detail,omitempty"`
}

func (p *EventPublisher) Publish(ctx context.Context, e biz.Event) error {
	payload, err := json.Marshal(wireEvent{
		Time:       e.Time.Format("2006-01-02T15:04:05.000Z07:00"),
		SessionID:  e.SessionID,
		MachineMAC: e.MachineMAC,
		Phase:      string(e.Phase),
		Outcome:    string(e.Outcome),
		Detail:     e.Detail,
	})
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	// Sharded pub/sub (SPUBLISH), not plain PUBLISH: on a Valkey cluster the
	// regular PUBLISH cluster-bus fan-out is unreliable through valkey-go and
	// intermittently returns EOF (~10% of calls in production on kmx03), which
	// silently drops live events from the SSE stream. SPUBLISH pins the channel
	// to a single shard by slot; the SSE subscriber uses the matching SSUBSCRIBE
	// so both land on the same shard. See sse.go.
	cmd := p.data.Valkey.B().Spublish().Channel(EventsChannel).Message(string(payload)).Build()
	if err := p.data.Valkey.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("publish event: %w", err)
	}
	return nil
}
