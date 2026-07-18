package data

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/valkey-io/valkey-go"

	"universe/backend/internal/biz"
)

// LeaseRepo reads active leases from Valkey (biz.LeaseReader).
type LeaseRepo struct {
	data *Data
}

func NewLeaseRepo(d *Data) *LeaseRepo { return &LeaseRepo{data: d} }

type storedLease struct {
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	MachineID string `json:"machine_id"`
	ExpiresAt string `json:"expires_at"`
}

// ListLeases scans lease:* keys. Reverse-index keys (lease:mac:*) are skipped.
func (r *LeaseRepo) ListLeases(ctx context.Context, page, pageSize int) ([]biz.Lease, int64, error) {
	var all []biz.Lease
	var cursor uint64
	for {
		res := r.data.Valkey.Do(ctx, r.data.Valkey.B().Scan().Cursor(cursor).
			Match("lease:*").Count(200).Build())
		entry, err := res.AsScanEntry()
		if err != nil {
			return nil, 0, fmt.Errorf("scan leases: %w", err)
		}
		for _, key := range entry.Elements {
			if isReverseIndex(key) {
				continue
			}
			lease, err := r.readLease(ctx, key)
			if err != nil || lease == nil {
				continue
			}
			all = append(all, *lease)
		}
		cursor = entry.Cursor
		if cursor == 0 {
			break
		}
	}

	total := int64(len(all))
	start, end := paginate(len(all), page, pageSize)
	return all[start:end], total, nil
}

func (r *LeaseRepo) readLease(ctx context.Context, key string) (*biz.Lease, error) {
	raw, err := r.data.Valkey.Do(ctx, r.data.Valkey.B().Get().Key(key).Build()).ToString()
	if err != nil {
		if err == valkey.Nil {
			return nil, nil
		}
		return nil, err
	}
	var sl storedLease
	if err := json.Unmarshal([]byte(raw), &sl); err != nil {
		return nil, err
	}
	return &biz.Lease{IP: sl.IP, MAC: sl.MAC, MachineID: sl.MachineID}, nil
}

func isReverseIndex(key string) bool {
	const prefix = "lease:mac:"
	return len(key) >= len(prefix) && key[:len(prefix)] == prefix
}

func paginate(n, page, pageSize int) (int, int) {
	p, size := normalizePage(page, pageSize)
	start := min((p-1)*size, n)
	end := min(start+size, n)
	return start, end
}
