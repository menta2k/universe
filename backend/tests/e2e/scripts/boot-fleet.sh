#!/usr/bin/env bash
# Boot N VMs concurrently to exercise parallel provisioning (SC-003).
# Usage: boot-fleet.sh --count 10
set -euo pipefail

COUNT=10
while [[ $# -gt 0 ]]; do
  case "$1" in
    --count) COUNT="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
pids=()
for i in $(seq 1 "$COUNT"); do
  mac=$(printf '52:54:00:ab:cd:%02x' "$i")
  "$HERE/boot-vm.sh" --mac "$mac" --firmware uefi >"/tmp/nbfleet-$i.log" 2>&1 &
  pids+=($!)
done
echo "launched ${#pids[@]} VMs; waiting..."
wait "${pids[@]}"
echo "fleet finished"
