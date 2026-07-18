CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TYPE firmware_type AS ENUM ('bios', 'uefi_x64', 'unknown');
CREATE TYPE provision_state AS ENUM ('new', 'ready', 'installing', 'installed', 'failed');
CREATE TYPE ubuntu_release AS ENUM ('jammy', 'noble');
CREATE TYPE session_state AS ENUM ('active', 'completed', 'failed', 'stale');
CREATE TYPE artifact_kind AS ENUM ('kernel', 'initrd', 'ipxe_bin', 'other');

CREATE TABLE operators (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username      text NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9._-]{3,64}$'),
    password_hash text NOT NULL,
    display_name  text NOT NULL DEFAULT '',
    active        boolean NOT NULL DEFAULT true,
    last_login_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE profiles (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                text NOT NULL UNIQUE,
    version             integer NOT NULL DEFAULT 1,
    ubuntu_release      ubuntu_release NOT NULL,
    storage_layout      jsonb NOT NULL DEFAULT '{"mode":"lvm"}',
    network_config      jsonb NOT NULL DEFAULT '{}',
    packages            text[] NOT NULL DEFAULT '{}',
    ssh_authorized_keys text[] NOT NULL CHECK (cardinality(ssh_authorized_keys) >= 1),
    user_data_template  text,
    late_commands       text[] NOT NULL DEFAULT '{}',
    kernel_cmdline_extra text NOT NULL DEFAULT '' CHECK (kernel_cmdline_extra !~ '[\n\r]'),
    created_by          uuid REFERENCES operators(id),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE profile_revisions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id     uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    version        integer NOT NULL,
    snapshot       jsonb NOT NULL,
    revised_by     uuid REFERENCES operators(id),
    revised_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (profile_id, version)
);

CREATE TABLE machines (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    mac             macaddr NOT NULL UNIQUE,
    name            text NOT NULL UNIQUE CHECK (name ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
    firmware        firmware_type NOT NULL DEFAULT 'unknown',
    profile_id      uuid REFERENCES profiles(id) ON DELETE RESTRICT,
    reservation_ip  inet UNIQUE,
    provision_state provision_state NOT NULL DEFAULT 'new',
    notes           text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE dhcp_config (
    id                 boolean PRIMARY KEY DEFAULT true CHECK (id), -- single row
    enabled            boolean NOT NULL DEFAULT false,
    version            integer NOT NULL DEFAULT 1,
    lease_ttl_seconds  integer NOT NULL DEFAULT 3600
        CHECK (lease_ttl_seconds BETWEEN 300 AND 86400),
    updated_by         uuid REFERENCES operators(id),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
INSERT INTO dhcp_config DEFAULT VALUES;

CREATE TABLE dhcp_subnets (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    network     cidr NOT NULL UNIQUE,
    range_start inet NOT NULL,
    range_end   inet NOT NULL,
    gateway     inet,
    dns         inet[] NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (range_start <<= network AND range_end <<= network AND range_start <= range_end)
);

CREATE TABLE dhcp_reservations (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id uuid NOT NULL UNIQUE REFERENCES machines(id) ON DELETE CASCADE,
    ip         inet NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE boot_artifacts (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind           artifact_kind NOT NULL,
    ubuntu_release ubuntu_release,
    filename       text NOT NULL UNIQUE CHECK (filename ~ '^[A-Za-z0-9._-]+$'),
    path           text NOT NULL,
    size_bytes     bigint NOT NULL CHECK (size_bytes > 0),
    sha256         text NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    uploaded_by    uuid REFERENCES operators(id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CHECK (kind NOT IN ('kernel', 'initrd') OR ubuntu_release IS NOT NULL)
);

CREATE TABLE provisioning_sessions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id      uuid NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    profile_id      uuid NOT NULL REFERENCES profiles(id),
    profile_version integer NOT NULL,
    state           session_state NOT NULL DEFAULT 'active',
    started_at      timestamptz NOT NULL DEFAULT now(),
    ended_at        timestamptz,
    failure_phase   text,
    evidence        jsonb NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX one_active_session_per_machine
    ON provisioning_sessions (machine_id) WHERE state = 'active';
