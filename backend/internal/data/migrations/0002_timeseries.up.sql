CREATE TYPE event_phase AS ENUM (
    'dhcp_discover', 'dhcp_offer', 'dhcp_ack', 'lease_granted', 'lease_expired',
    'tftp_transfer', 'ipxe_script', 'file_served', 'seed_served', 'install_report',
    'session_completed', 'session_failed', 'session_stale',
    'unknown_machine', 'foreign_dhcp_detected', 'config_change'
);
CREATE TYPE event_outcome AS ENUM ('ok', 'error', 'denied');

CREATE TABLE provisioning_events (
    time        timestamptz NOT NULL DEFAULT now(),
    session_id  uuid,
    machine_mac macaddr,
    phase       event_phase NOT NULL,
    outcome     event_outcome NOT NULL DEFAULT 'ok',
    detail      jsonb NOT NULL DEFAULT '{}'
);
SELECT create_hypertable('provisioning_events', 'time');
CREATE INDEX provisioning_events_session_idx ON provisioning_events (session_id, time DESC);
CREATE INDEX provisioning_events_mac_idx ON provisioning_events (machine_mac, time DESC);
CREATE INDEX provisioning_events_phase_idx ON provisioning_events (phase, time DESC);

CREATE TABLE tftp_transfers (
    time       timestamptz NOT NULL DEFAULT now(),
    client_ip  inet NOT NULL,
    filename   text NOT NULL,
    bytes_sent bigint NOT NULL DEFAULT 0,
    success    boolean NOT NULL,
    error      text NOT NULL DEFAULT ''
);
SELECT create_hypertable('tftp_transfers', 'time');
CREATE INDEX tftp_transfers_client_idx ON tftp_transfers (client_ip, time DESC);

CREATE TABLE dhcp_offers_seen (
    time       timestamptz NOT NULL DEFAULT now(),
    server_id  inet NOT NULL,
    client_mac macaddr NOT NULL,
    offered_ip inet
);
SELECT create_hypertable('dhcp_offers_seen', 'time');
CREATE INDEX dhcp_offers_seen_server_idx ON dhcp_offers_seen (server_id, time DESC);

-- Retention: the '90 days' literal is the default; the application updates the
-- policy from events.retention_days at startup.
SELECT add_retention_policy('provisioning_events', INTERVAL '90 days');
SELECT add_retention_policy('tftp_transfers', INTERVAL '90 days');
SELECT add_retention_policy('dhcp_offers_seen', INTERVAL '90 days');
