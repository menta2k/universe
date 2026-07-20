#!/bin/sh
# Post-install: register the systemd unit. On a fresh install the service is
# NOT started automatically (config/migrations come first). On an UPGRADE of an
# already-running service we restart it, so the new binary actually takes over
# instead of leaving the old one in memory.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    if systemctl is-active --quiet netbootd 2>/dev/null; then
        echo "netbootd upgraded — restarting the running service."
        systemctl restart netbootd || true
        exit 0
    fi
fi

cat <<'EOF'
netbootd installed.

Next steps:
  1. Edit /etc/netbootd/netbootd.yaml
  2. netbootd migrate -conf /etc/netbootd/netbootd.yaml
  3. systemctl enable --now netbootd
EOF
