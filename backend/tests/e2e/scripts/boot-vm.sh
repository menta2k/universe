#!/usr/bin/env bash
# Boot a diskless QEMU VM that PXE-boots against the netbootd on the test bridge.
# Usage: boot-vm.sh --mac <mac> --firmware <bios|uefi> [--disk <path>]
#
# The VM has an empty disk and boots from the network first, so it exercises
# the full DHCP -> TFTP(iPXE) -> HTTP(kernel/initrd/seed) -> autoinstall path.
set -euo pipefail

MAC=""
FIRMWARE="uefi"
DISK=""
BRIDGE="${BRIDGE:-nbtest0}"
MEM="${MEM:-4096}"
OVMF="${OVMF:-/usr/share/OVMF/OVMF_CODE.fd}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mac) MAC="$2"; shift 2;;
    --firmware) FIRMWARE="$2"; shift 2;;
    --disk) DISK="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done

[[ -z "$MAC" ]] && { echo "--mac is required" >&2; exit 2; }
if [[ -z "$DISK" ]]; then
  DISK="$(mktemp --suffix=.qcow2)"
  qemu-img create -f qcow2 "$DISK" 20G >/dev/null
fi

FW_ARGS=()
if [[ "$FIRMWARE" == "uefi" ]]; then
  FW_ARGS=(-drive "if=pflash,format=raw,readonly=on,file=${OVMF}")
fi

# net boot order (bootindex on the NIC) forces PXE first.
exec qemu-system-x86_64 \
  -enable-kvm -m "$MEM" -smp 2 \
  "${FW_ARGS[@]}" \
  -drive "file=${DISK},if=virtio,format=qcow2" \
  -netdev "bridge,id=n0,br=${BRIDGE}" \
  -device "virtio-net-pci,netdev=n0,mac=${MAC},bootindex=0" \
  -nographic -serial mon:stdio
