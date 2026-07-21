package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/menta2k/universe/backend/internal/biz"
)

// MachineRepo is the pgx implementation of biz.MachineRepo.
type MachineRepo struct {
	data *Data
}

func NewMachineRepo(d *Data) *MachineRepo { return &MachineRepo{data: d} }

const machineCols = `id, mac::text, name, firmware, coalesce(profile_id::text,''),
	coalesce(host(reservation_ip),''), provision_state, notes, network_config,
	install_network, created_at, updated_at`

func scanMachine(row pgx.Row) (*biz.Machine, error) {
	var m biz.Machine
	var network, installNet []byte
	err := row.Scan(&m.ID, &m.MAC, &m.Name, &m.Firmware, &m.ProfileID,
		&m.ReservationIP, &m.State, &m.Notes, &network, &installNet, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, biz.ErrEntityNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan machine: %w", err)
	}
	if len(network) > 0 && string(network) != "{}" {
		if err := json.Unmarshal(network, &m.NetworkConfig); err != nil {
			return nil, fmt.Errorf("decode machine network config: %w", err)
		}
	}
	if len(installNet) > 0 && string(installNet) != "{}" {
		if err := json.Unmarshal(installNet, &m.InstallNetwork); err != nil {
			return nil, fmt.Errorf("decode machine install network: %w", err)
		}
	}
	return &m, nil
}

func (r *MachineRepo) GetByID(ctx context.Context, id string) (*biz.Machine, error) {
	return scanMachine(r.data.Pool.QueryRow(ctx,
		`SELECT `+machineCols+` FROM machines WHERE id = $1`, id))
}

func (r *MachineRepo) GetByMAC(ctx context.Context, mac string) (*biz.Machine, error) {
	return scanMachine(r.data.Pool.QueryRow(ctx,
		`SELECT `+machineCols+` FROM machines WHERE mac = $1::macaddr`, mac))
}

func (r *MachineRepo) List(ctx context.Context, f biz.MachineFilter) ([]*biz.Machine, int64, error) {
	where := []string{"true"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.State != "" {
		where = append(where, "provision_state = "+arg(string(f.State))+"::provision_state")
	}
	if f.ProfileID != "" {
		where = append(where, "profile_id = "+arg(f.ProfileID)+"::uuid")
	}
	if f.Query != "" {
		where = append(where, "(name ILIKE "+arg("%"+f.Query+"%")+" OR mac::text ILIKE "+arg("%"+f.Query+"%")+")")
	}
	cond := strings.Join(where, " AND ")

	var total int64
	if err := r.data.Pool.QueryRow(ctx,
		"SELECT count(*) FROM machines WHERE "+cond, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count machines: %w", err)
	}

	page, size := normalizePage(f.Page, f.PageSize)
	query := "SELECT " + machineCols + " FROM machines WHERE " + cond +
		" ORDER BY name LIMIT " + arg(size) + " OFFSET " + arg((page-1)*size)
	rows, err := r.data.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list machines: %w", err)
	}
	defer rows.Close()

	var out []*biz.Machine
	for rows.Next() {
		m, err := scanMachine(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (r *MachineRepo) Create(ctx context.Context, m *biz.Machine) (*biz.Machine, error) {
	network, err := json.Marshal(orEmptyMap(m.NetworkConfig))
	if err != nil {
		return nil, fmt.Errorf("marshal machine network config: %w", err)
	}
	installNet, err := json.Marshal(m.InstallNetwork)
	if err != nil {
		return nil, fmt.Errorf("marshal machine install network: %w", err)
	}
	created, err := scanMachine(r.data.Pool.QueryRow(ctx,
		`INSERT INTO machines (mac, name, firmware, profile_id, reservation_ip, provision_state, notes, network_config, install_network)
		 VALUES ($1::macaddr, $2, $3, NULLIF($4,'')::uuid, NULLIF($5,'')::inet, $6, $7, $8, $9)
		 RETURNING `+machineCols,
		m.MAC, m.Name, string(m.Firmware), m.ProfileID, m.ReservationIP, string(m.State), m.Notes, network, installNet))
	if err != nil {
		return nil, wrapConstraint(err, map[string]string{
			"machines_mac_key":            "mac already registered",
			"machines_name_key":           "name already in use",
			"machines_reservation_ip_key": "reservation IP already in use",
		})
	}
	return created, nil
}

func (r *MachineRepo) Update(ctx context.Context, id string, u biz.MachineUpdate) (*biz.Machine, error) {
	set := []string{"updated_at = now()"}
	args := []any{id}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if u.Name != nil {
		set = append(set, "name = "+arg(*u.Name))
	}
	if u.ProfileID != nil {
		set = append(set, "profile_id = NULLIF("+arg(*u.ProfileID)+",'')::uuid")
	}
	if u.ReservationIP != nil {
		set = append(set, "reservation_ip = NULLIF("+arg(*u.ReservationIP)+",'')::inet")
	}
	if u.Notes != nil {
		set = append(set, "notes = "+arg(*u.Notes))
	}
	if u.Firmware != nil {
		set = append(set, "firmware = "+arg(string(*u.Firmware))+"::firmware_type")
	}
	if u.State != nil {
		set = append(set, "provision_state = "+arg(string(*u.State))+"::provision_state")
	}
	if u.NetworkConfig != nil {
		network, err := json.Marshal(orEmptyMap(*u.NetworkConfig))
		if err != nil {
			return nil, fmt.Errorf("marshal machine network config: %w", err)
		}
		set = append(set, "network_config = "+arg(network))
	}
	if u.InstallNetwork != nil {
		installNet, err := json.Marshal(*u.InstallNetwork)
		if err != nil {
			return nil, fmt.Errorf("marshal machine install network: %w", err)
		}
		set = append(set, "install_network = "+arg(installNet))
	}
	return scanMachine(r.data.Pool.QueryRow(ctx,
		"UPDATE machines SET "+strings.Join(set, ", ")+" WHERE id = $1 RETURNING "+machineCols,
		args...))
}

func (r *MachineRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.data.Pool.Exec(ctx, `DELETE FROM machines WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete machine: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return biz.ErrEntityNotFound
	}
	return nil
}

// ListUnknownBoots aggregates unknown_machine events (FR-005).
func (r *MachineRepo) ListUnknownBoots(ctx context.Context, page, pageSize int) ([]*biz.UnknownBoot, int64, error) {
	p, size := normalizePage(page, pageSize)
	var total int64
	if err := r.data.Pool.QueryRow(ctx,
		`SELECT count(DISTINCT machine_mac) FROM provisioning_events
		 WHERE phase = 'unknown_machine'
		   AND machine_mac NOT IN (SELECT mac FROM machines)`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count unknown boots: %w", err)
	}
	rows, err := r.data.Pool.Query(ctx,
		`SELECT machine_mac::text, max(time), count(*) FROM provisioning_events
		 WHERE phase = 'unknown_machine'
		   AND machine_mac NOT IN (SELECT mac FROM machines)
		 GROUP BY machine_mac ORDER BY max(time) DESC LIMIT $1 OFFSET $2`,
		size, (p-1)*size)
	if err != nil {
		return nil, 0, fmt.Errorf("list unknown boots: %w", err)
	}
	defer rows.Close()
	var out []*biz.UnknownBoot
	for rows.Next() {
		var b biz.UnknownBoot
		if err := rows.Scan(&b.MAC, &b.LastSeen, &b.Attempts); err != nil {
			return nil, 0, fmt.Errorf("scan unknown boot: %w", err)
		}
		out = append(out, &b)
	}
	return out, total, rows.Err()
}

func normalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

// wrapConstraint converts unique-violation errors into field messages.
func wrapConstraint(err error, constraints map[string]string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if msg, ok := constraints[pgErr.ConstraintName]; ok {
			return &biz.ValidationError{Fields: map[string]string{pgErr.ConstraintName: msg}}
		}
	}
	return err
}
