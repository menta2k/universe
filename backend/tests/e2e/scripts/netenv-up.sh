#!/usr/bin/env bash
# Create an isolated host-only bridge for E2E netboot tests.
# The netbootd DHCP/TFTP/boot-HTTP services bind to this bridge so target VMs
# PXE-boot against them without touching any real network.
#
# Requires root (or CAP_NET_ADMIN) and: ip, qemu-system-x86_64, ovmf.
set -euo pipefail

BRIDGE="${BRIDGE:-nbtest0}"
HOST_IP="${HOST_IP:-192.168.90.1}"
PREFIX="${PREFIX:-24}"

if ip link show "$BRIDGE" >/dev/null 2>&1; then
  echo "bridge $BRIDGE already exists"
else
  ip link add name "$BRIDGE" type bridge
  ip addr add "${HOST_IP}/${PREFIX}" dev "$BRIDGE"
  ip link set "$BRIDGE" up
  echo "created bridge $BRIDGE with ${HOST_IP}/${PREFIX}"
fi

# Allow the netbootd on the host to bind privileged ports without root:
#   setcap 'cap_net_bind_service=+ep' backend/bin/netbootd
echo "bridge ready; point netbootd's dhcp_interface at $BRIDGE"
