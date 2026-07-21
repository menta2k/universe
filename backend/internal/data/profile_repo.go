package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/menta2k/universe/backend/internal/biz"
)

// ProfileRepo is the pgx implementation of biz.ProfileRepo (US1 subset:
// create/get/list; full lifecycle arrives with US2).
type ProfileRepo struct {
	data *Data
}

func NewProfileRepo(d *Data) *ProfileRepo { return &ProfileRepo{data: d} }

const profileCols = `p.id, p.name, p.version, p.ubuntu_release, p.storage_layout,
	p.network_config, p.packages, p.ssh_authorized_keys, coalesce(p.user_data_template,''),
	p.late_commands, p.kernel_cmdline_extra, p.keyboard_layout, p.keyboard_variant,
	p.locale, p.timezone, p.install_username, p.install_password_hash, p.default_dns,
	p.created_at, p.updated_at,
	(SELECT count(*) FROM machines m WHERE m.profile_id = p.id)`

func scanProfile(row pgx.Row) (*biz.Profile, error) {
	var p biz.Profile
	var storage, network []byte
	err := row.Scan(&p.ID, &p.Name, &p.Version, &p.UbuntuRelease, &storage,
		&network, &p.Packages, &p.SSHAuthorizedKeys, &p.UserDataTemplate,
		&p.LateCommands, &p.KernelCmdlineExtra, &p.KeyboardLayout, &p.KeyboardVariant,
		&p.Locale, &p.Timezone, &p.InstallUsername, &p.InstallPasswordHash, &p.DefaultDNS,
		&p.CreatedAt, &p.UpdatedAt, &p.AssignedMachines)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, biz.ErrEntityNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan profile: %w", err)
	}
	if err := json.Unmarshal(storage, &p.StorageLayout); err != nil {
		return nil, fmt.Errorf("decode storage layout: %w", err)
	}
	if len(network) > 0 && string(network) != "{}" {
		if err := json.Unmarshal(network, &p.NetworkConfig); err != nil {
			return nil, fmt.Errorf("decode network config: %w", err)
		}
	}
	return &p, nil
}

func (r *ProfileRepo) GetByID(ctx context.Context, id string) (*biz.Profile, error) {
	return scanProfile(r.data.Pool.QueryRow(ctx,
		`SELECT `+profileCols+` FROM profiles p WHERE p.id = $1`, id))
}

func (r *ProfileRepo) List(ctx context.Context, page, pageSize int) ([]*biz.Profile, int64, error) {
	var total int64
	if err := r.data.Pool.QueryRow(ctx, `SELECT count(*) FROM profiles`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count profiles: %w", err)
	}
	p, size := normalizePage(page, pageSize)
	rows, err := r.data.Pool.Query(ctx,
		`SELECT `+profileCols+` FROM profiles p ORDER BY p.name LIMIT $1 OFFSET $2`,
		size, (p-1)*size)
	if err != nil {
		return nil, 0, fmt.Errorf("list profiles: %w", err)
	}
	defer rows.Close()
	var out []*biz.Profile
	for rows.Next() {
		pr, err := scanProfile(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, pr)
	}
	return out, total, rows.Err()
}

func (r *ProfileRepo) Create(ctx context.Context, p *biz.Profile) (*biz.Profile, error) {
	storage, err := json.Marshal(p.StorageLayout)
	if err != nil {
		return nil, fmt.Errorf("marshal storage layout: %w", err)
	}
	network, err := json.Marshal(orEmptyMap(p.NetworkConfig))
	if err != nil {
		return nil, fmt.Errorf("marshal network config: %w", err)
	}
	created, err := scanProfile(r.data.Pool.QueryRow(ctx,
		`INSERT INTO profiles (name, ubuntu_release, storage_layout, network_config,
		   packages, ssh_authorized_keys, user_data_template, late_commands, kernel_cmdline_extra,
		   keyboard_layout, keyboard_variant, locale, timezone, install_username, install_password_hash,
		   default_dns)
		 VALUES ($1, $2::ubuntu_release, $3, $4, $5, $6, NULLIF($7,''), $8, $9, $10, $11, $12, $13, $14, $15, $16)
		 RETURNING `+profileColsSelf,
		p.Name, string(p.UbuntuRelease), storage, network, orEmptySlice(p.Packages),
		p.SSHAuthorizedKeys, p.UserDataTemplate, orEmptySlice(p.LateCommands),
		p.KernelCmdlineExtra, defaultStr(p.KeyboardLayout, "us"), p.KeyboardVariant,
		defaultStr(p.Locale, "en_US.UTF-8"), p.Timezone,
		p.InstallUsername, p.InstallPasswordHash, orEmptySlice(p.DefaultDNS)))
	if err != nil {
		return nil, wrapConstraint(err, map[string]string{
			"profiles_name_key": "profile name already in use",
		})
	}
	return created, nil
}

// Update writes a new version and archives the prior state into
// profile_revisions, transactionally.
func (r *ProfileRepo) Update(ctx context.Context, p *biz.Profile) (*biz.Profile, error) {
	storage, err := json.Marshal(p.StorageLayout)
	if err != nil {
		return nil, fmt.Errorf("marshal storage layout: %w", err)
	}
	network, err := json.Marshal(orEmptyMap(p.NetworkConfig))
	if err != nil {
		return nil, fmt.Errorf("marshal network config: %w", err)
	}
	tx, err := r.data.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Snapshot the current row into revisions before overwriting.
	if _, err := tx.Exec(ctx,
		`INSERT INTO profile_revisions (profile_id, version, snapshot)
		 SELECT id, version, to_jsonb(profiles.*) FROM profiles WHERE id = $1`, p.ID); err != nil {
		return nil, fmt.Errorf("archive revision: %w", err)
	}
	updated, err := scanProfile(tx.QueryRow(ctx,
		`UPDATE profiles SET version = $2, ubuntu_release = $3::ubuntu_release,
		   storage_layout = $4, network_config = $5, packages = $6,
		   ssh_authorized_keys = $7, user_data_template = NULLIF($8,''),
		   late_commands = $9, kernel_cmdline_extra = $10,
		   keyboard_layout = $11, keyboard_variant = $12, locale = $13, timezone = $14,
		   install_username = $15, install_password_hash = $16, default_dns = $17,
		   updated_at = now()
		 WHERE id = $1 RETURNING `+profileColsSelf,
		p.ID, p.Version, string(p.UbuntuRelease), storage, network,
		orEmptySlice(p.Packages), p.SSHAuthorizedKeys, p.UserDataTemplate,
		orEmptySlice(p.LateCommands), p.KernelCmdlineExtra,
		defaultStr(p.KeyboardLayout, "us"), p.KeyboardVariant,
		defaultStr(p.Locale, "en_US.UTF-8"), p.Timezone,
		p.InstallUsername, p.InstallPasswordHash, orEmptySlice(p.DefaultDNS)))
	if err != nil {
		return nil, wrapConstraint(err, map[string]string{
			"profiles_name_key": "profile name already in use"})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit profile update: %w", err)
	}
	return updated, nil
}

// Delete removes a profile, mapping the FK RESTRICT violation to ErrProfileInUse.
func (r *ProfileRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.data.Pool.Exec(ctx, `DELETE FROM profiles WHERE id = $1`, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return biz.ErrProfileInUse
		}
		return fmt.Errorf("delete profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return biz.ErrEntityNotFound
	}
	return nil
}

// profileColsSelf is profileCols without the table alias, for RETURNING.
const profileColsSelf = `id, name, version, ubuntu_release, storage_layout,
	network_config, packages, ssh_authorized_keys, coalesce(user_data_template,''),
	late_commands, kernel_cmdline_extra, keyboard_layout, keyboard_variant,
	locale, timezone, install_username, install_password_hash, default_dns,
	created_at, updated_at, 0::bigint`

// defaultStr returns fallback when s is empty.
func defaultStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func orEmptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
