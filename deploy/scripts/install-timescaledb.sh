#!/usr/bin/env bash
# Install and configure TimescaleDB for netbootd.
#
# Modes:
#   docker  (default) — run the timescale/timescaledb:2.17.2-pg16 container,
#                       the same image CI and docker-compose.prod.yml use.
#   native            — install PostgreSQL 16 + TimescaleDB from the official
#                       apt repositories (Ubuntu/Debian), tune, and create the
#                       database. Requires sudo.
#
# Usage:
#   ./install-timescaledb.sh [--mode docker|native] [--db netboot]
#       [--user netboot] [--password <pw>] [--port 5432] [--subnet <cidr>]
#
# If --password is omitted a random one is generated and printed at the end.
# After the script finishes, put the printed DSN into netbootd.yaml and run:
#   netbootd migrate -conf /etc/netbootd/netbootd.yaml

set -euo pipefail

MODE="docker"
DB_NAME="netboot"
DB_USER="netboot"
DB_PASSWORD=""
DB_PORT="5432"
CLIENT_SUBNET=""            # native mode: optional pg_hba network rule
IMAGE="timescale/timescaledb:2.17.2-pg16"
CONTAINER_NAME="timescaledb"
VOLUME_NAME="timescale-data"
PG_VERSION="16"

log()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# SUDO is empty when already root, "sudo" otherwise. Used for system commands
# (apt, systemctl) in native mode.
if [[ "$(id -u)" -eq 0 ]]; then SUDO=""; else SUDO="sudo"; fi

# run_pg_super runs a command as the postgres OS user (the peer-auth superuser),
# working whether the script runs as root or under sudo.
run_pg_super() {
    if [[ "$(id -u)" -eq 0 ]]; then
        if command -v runuser >/dev/null 2>&1; then
            runuser -u postgres -- "$@"
        else
            su -s /bin/sh postgres -c "$(printf '%q ' "$@")"
        fi
    else
        sudo -u postgres "$@"
    fi
}

usage() { sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'; exit 0; }

while [[ $# -gt 0 ]]; do
    case "$1" in
        --mode)     MODE="$2"; shift 2 ;;
        --db)       DB_NAME="$2"; shift 2 ;;
        --user)     DB_USER="$2"; shift 2 ;;
        --password) DB_PASSWORD="$2"; shift 2 ;;
        --port)     DB_PORT="$2"; shift 2 ;;
        --subnet)   CLIENT_SUBNET="$2"; shift 2 ;;
        -h|--help)  usage ;;
        *)          die "unknown option: $1 (try --help)" ;;
    esac
done

[[ "$MODE" == "docker" || "$MODE" == "native" ]] || die "--mode must be docker or native"

if [[ -z "$DB_PASSWORD" ]]; then
    DB_PASSWORD=$(head -c 32 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24)
    GENERATED_PASSWORD=1
else
    GENERATED_PASSWORD=0
fi

wait_for_db() {
    local check=("$@")
    log "Waiting for PostgreSQL to accept connections..."
    for _ in $(seq 1 30); do
        if "${check[@]}" >/dev/null 2>&1; then return 0; fi
        sleep 2
    done
    die "database did not become ready within 60s"
}

install_docker() {
    command -v docker >/dev/null || die "docker is not installed"

    if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
        die "container '$CONTAINER_NAME' already exists — remove it first or pick another name"
    fi

    log "Starting $IMAGE as '$CONTAINER_NAME' on 127.0.0.1:$DB_PORT"
    docker run -d --name "$CONTAINER_NAME" \
        --restart unless-stopped \
        -e POSTGRES_DB="$DB_NAME" \
        -e POSTGRES_USER="$DB_USER" \
        -e POSTGRES_PASSWORD="$DB_PASSWORD" \
        -p "127.0.0.1:${DB_PORT}:5432" \
        -v "${VOLUME_NAME}:/var/lib/postgresql/data" \
        "$IMAGE" >/dev/null

    wait_for_db docker exec "$CONTAINER_NAME" pg_isready -U "$DB_USER" -d "$DB_NAME"
    # The image preloads the extension; create it so the netbootd migration
    # (CREATE EXTENSION IF NOT EXISTS) is a no-op regardless of privileges.
    docker exec "$CONTAINER_NAME" psql -U "$DB_USER" -d "$DB_NAME" \
        -c "CREATE EXTENSION IF NOT EXISTS timescaledb;" >/dev/null
}

install_native() {
    [[ "$(id -u)" -eq 0 ]] || command -v sudo >/dev/null || die "needs root or sudo"
    command -v lsb_release >/dev/null || die "lsb_release not found (Ubuntu/Debian only)"
    local codename; codename=$(lsb_release -cs)

    log "Adding PostgreSQL (PGDG) apt repository"
    $SUDO apt-get install -y -qq curl ca-certificates gnupg >/dev/null
    curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc |
        $SUDO gpg --dearmor --yes -o /usr/share/keyrings/pgdg.gpg
    echo "deb [signed-by=/usr/share/keyrings/pgdg.gpg] http://apt.postgresql.org/pub/repos/apt ${codename}-pgdg main" |
        $SUDO tee /etc/apt/sources.list.d/pgdg.list >/dev/null

    log "Adding TimescaleDB apt repository"
    curl -fsSL https://packagecloud.io/timescale/timescaledb/gpgkey |
        $SUDO gpg --dearmor --yes -o /usr/share/keyrings/timescaledb.gpg
    echo "deb [signed-by=/usr/share/keyrings/timescaledb.gpg] https://packagecloud.io/timescale/timescaledb/ubuntu/ ${codename} main" |
        $SUDO tee /etc/apt/sources.list.d/timescaledb.list >/dev/null

    log "Installing timescaledb-2-postgresql-${PG_VERSION}"
    $SUDO apt-get update -qq
    $SUDO apt-get install -y -qq "timescaledb-2-postgresql-${PG_VERSION}" "postgresql-client-${PG_VERSION}" >/dev/null

    log "Tuning postgresql.conf (timescaledb-tune)"
    $SUDO timescaledb-tune --quiet --yes >/dev/null

    local conf="/etc/postgresql/${PG_VERSION}/main/postgresql.conf"
    local hba="/etc/postgresql/${PG_VERSION}/main/pg_hba.conf"

    if [[ "$DB_PORT" != "5432" ]]; then
        log "Setting listen port to $DB_PORT"
        $SUDO sed -i "s/^#\?port\s*=.*/port = ${DB_PORT}/" "$conf"
    fi

    if [[ -n "$CLIENT_SUBNET" ]]; then
        log "Allowing $DB_USER@$CLIENT_SUBNET in pg_hba.conf and listening on all addresses"
        if ! $SUDO grep -q "host  ${DB_NAME}  ${DB_USER}  ${CLIENT_SUBNET}" "$hba"; then
            echo "host  ${DB_NAME}  ${DB_USER}  ${CLIENT_SUBNET}  scram-sha-256" | $SUDO tee -a "$hba" >/dev/null
        fi
        $SUDO sed -i "s/^#\?listen_addresses.*/listen_addresses = '*'/" "$conf"
    fi

    log "Restarting PostgreSQL"
    $SUDO systemctl restart postgresql
    wait_for_db run_pg_super pg_isready -p "$DB_PORT"

    log "Creating role '$DB_USER' and database '$DB_NAME'"
    run_pg_super psql -p "$DB_PORT" -v ON_ERROR_STOP=1 \
        -v user="$DB_USER" -v pass="$DB_PASSWORD" -v db="$DB_NAME" <<'SQL' >/dev/null
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'user', :'pass')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'user') \gexec
SELECT format('CREATE DATABASE %I OWNER %I', :'db', :'user')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'db') \gexec
SQL
    # Pre-create the extension as superuser so netbootd's migration needs no
    # elevated privileges.
    run_pg_super psql -p "$DB_PORT" -d "$DB_NAME" \
        -c "CREATE EXTENSION IF NOT EXISTS timescaledb;" >/dev/null
}

case "$MODE" in
    docker) install_docker ;;
    native) install_native ;;
esac

DSN="postgres://${DB_USER}:${DB_PASSWORD}@127.0.0.1:${DB_PORT}/${DB_NAME}?sslmode=disable"

log "TimescaleDB is ready."
echo
[[ "$GENERATED_PASSWORD" -eq 1 ]] && printf 'Generated password: %s\n\n' "$DB_PASSWORD"
cat <<EOF
Add to /etc/netbootd/netbootd.yaml:

  database:
    dsn: "${DSN}"

Then apply the embedded migrations:

  netbootd migrate -conf /etc/netbootd/netbootd.yaml

Verify:

  psql "${DSN}" -c '\dx timescaledb'
EOF
