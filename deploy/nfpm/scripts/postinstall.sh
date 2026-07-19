#!/bin/sh
# Post-install: register the systemd unit. The service is NOT started
# automatically — edit /etc/netbootd/netbootd.yaml (DSN, secrets, interfaces)
# first, apply migrations, then enable it.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

cat <<'EOF'
netbootd installed.

Next steps:
  1. Edit /etc/netbootd/netbootd.yaml
  2. netbootd migrate -conf /etc/netbootd/netbootd.yaml
  3. systemctl enable --now netbootd
EOF
