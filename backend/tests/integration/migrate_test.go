package integration

import (
	"context"
	"testing"

	"universe/backend/tests/integration/testenv"
)

func TestMigrationsApplyAndSchemaIsUsable(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()

	var hypertables int
	err := env.Data.Pool.QueryRow(ctx,
		`SELECT count(*) FROM timescaledb_information.hypertables
		 WHERE hypertable_name IN ('provisioning_events','tftp_transfers','dhcp_offers_seen')`,
	).Scan(&hypertables)
	if err != nil {
		t.Fatalf("query hypertables: %v", err)
	}
	if hypertables != 3 {
		t.Errorf("expected 3 hypertables, got %d", hypertables)
	}

	var enabled bool
	if err := env.Data.Pool.QueryRow(ctx,
		`SELECT enabled FROM dhcp_config`).Scan(&enabled); err != nil {
		t.Fatalf("query dhcp_config: %v", err)
	}
	if enabled {
		t.Error("dhcp_config.enabled must default to false (FR-016)")
	}

	// Constraint smoke checks: profile requires >=1 ssh key.
	_, err = env.Data.Pool.Exec(ctx,
		`INSERT INTO profiles (name, ubuntu_release, ssh_authorized_keys)
		 VALUES ('bad', 'noble', '{}')`)
	if err == nil {
		t.Error("expected CHECK violation for empty ssh_authorized_keys")
	}
}
