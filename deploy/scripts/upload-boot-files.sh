#!/usr/bin/env bash
# Fetch the Ubuntu live-server kernel + initrd for a release and upload them to
# netbootd as boot-file artifacts. This is the step that makes provisioning
# actually boot: iPXE fetches /boot/file/<release>/kernel and .../initrd, so
# both must exist for the target release or the client hangs on "installing".
#
# The iPXE binaries (undionly.kpxe / ipxe.efi) are embedded in netbootd and are
# NOT uploaded here.
#
# Usage:
#   ./upload-boot-files.sh --url http://HOST:8080 --user admin --password PW \
#       --release noble [--iso /path/or/URL] [--keep]
#
#   --release   noble (24.04) | jammy (22.04)
#   --iso       optional: a local ISO path or a URL to use instead of the
#               auto-discovered latest live-server ISO for the release
#   --keep      keep the downloaded ISO / extracted files (default: temp, cleaned)
#
# Requires: curl, and one of: bsdtar (libarchive-tools) | 7z (p7zip) | xorriso.

set -euo pipefail

BASE="" USER_NAME="admin" PASSWORD="" RELEASE="" ISO_SRC="" KEEP=0

log()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
    case "$1" in
        --url)      BASE="$2"; shift 2 ;;
        --user)     USER_NAME="$2"; shift 2 ;;
        --password) PASSWORD="$2"; shift 2 ;;
        --release)  RELEASE="$2"; shift 2 ;;
        --iso)      ISO_SRC="$2"; shift 2 ;;
        --keep)     KEEP=1; shift ;;
        -h|--help)  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *)          die "unknown option: $1 (try --help)" ;;
    esac
done

[[ -n "$BASE" ]]     || die "--url is required (e.g. http://185.117.188.10:8080)"
[[ -n "$PASSWORD" ]] || die "--password is required"
case "$RELEASE" in
    noble) VER="24.04" ;;
    jammy) VER="22.04" ;;
    *)     die "--release must be noble or jammy" ;;
esac
command -v curl >/dev/null || die "curl is required"

# extractor picks whichever ISO reader is installed.
EXTRACT=""
if command -v bsdtar >/dev/null; then EXTRACT=bsdtar
elif command -v 7z >/dev/null; then EXTRACT=7z
elif command -v xorriso >/dev/null; then EXTRACT=xorriso
else die "need one of: bsdtar (apt install libarchive-tools), 7z, or xorriso"; fi

WORK=$(mktemp -d)
cleanup() { [[ "$KEEP" -eq 1 ]] || rm -rf "$WORK"; }
trap cleanup EXIT

# 1. Resolve + fetch the ISO -------------------------------------------------
ISO="$WORK/ubuntu.iso"
if [[ -n "$ISO_SRC" && -f "$ISO_SRC" ]]; then
    log "Using local ISO: $ISO_SRC"
    ISO="$ISO_SRC"
else
    local_url="$ISO_SRC"
    if [[ -z "$local_url" ]]; then
        log "Discovering latest $RELEASE ($VER) live-server ISO"
        name=$(curl -fsSL "https://releases.ubuntu.com/${VER}/" \
            | grep -oE "ubuntu-${VER}[0-9.]*-live-server-amd64\.iso" | sort -u | tail -1)
        [[ -n "$name" ]] || die "could not find a live-server ISO under releases.ubuntu.com/${VER}/"
        local_url="https://releases.ubuntu.com/${VER}/${name}"
    fi
    log "Downloading $local_url"
    curl -fSL --retry 3 -o "$ISO" "$local_url"
fi

# 2. Extract casper/vmlinuz + casper/initrd ----------------------------------
KERNEL="$WORK/kernel" INITRD="$WORK/initrd"
log "Extracting casper/vmlinuz and casper/initrd ($EXTRACT)"
case "$EXTRACT" in
    bsdtar)
        bsdtar -xOf "$ISO" casper/vmlinuz > "$KERNEL"
        bsdtar -xOf "$ISO" casper/initrd  > "$INITRD"
        ;;
    7z)
        7z e -so "$ISO" casper/vmlinuz > "$KERNEL" 2>/dev/null
        7z e -so "$ISO" casper/initrd  > "$INITRD" 2>/dev/null
        ;;
    xorriso)
        xorriso -osirrox on -indev "$ISO" -extract /casper/vmlinuz "$KERNEL" >/dev/null 2>&1
        xorriso -osirrox on -indev "$ISO" -extract /casper/initrd  "$INITRD" >/dev/null 2>&1
        ;;
esac
[[ -s "$KERNEL" ]] || die "extracted kernel is empty (casper/vmlinuz not found in ISO)"
[[ -s "$INITRD" ]] || die "extracted initrd is empty (casper/initrd not found in ISO)"
log "kernel $(du -h "$KERNEL" | cut -f1), initrd $(du -h "$INITRD" | cut -f1)"

# 3. Log in + upload ---------------------------------------------------------
CK="$WORK/cookies"
log "Authenticating to $BASE"
curl -fsS -c "$CK" -X POST "$BASE/api/v1/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"$USER_NAME\",\"password\":\"$PASSWORD\"}" >/dev/null \
    || die "login failed"

upload() {
    local kind="$1" file="$2"
    log "Uploading $kind ($RELEASE)"
    curl -fsS -b "$CK" -X POST "$BASE/api/v1/artifacts" \
        -F "kind=$kind" -F "ubuntu_release=$RELEASE" -F "file=@$file" \
        | grep -q '"success":true' || die "$kind upload failed"
}
upload kernel "$KERNEL"
upload initrd "$INITRD"

log "Done. $RELEASE kernel + initrd are uploaded."
cat <<EOF

Verify:  curl -s -b <cookie> "$BASE/api/v1/artifacts?page=1&page_size=50"
Now re-provision the machine (or reboot it if already armed) and it should
chainload the installer.

Note: if the installer boots but can't find its root filesystem, add the
live-server ISO URL to the profile's "Kernel cmdline extra", e.g.:
    url=$([[ -n "${local_url:-}" ]] && echo "$local_url" || echo "https://releases.ubuntu.com/${VER}/<iso>") ip=dhcp
EOF
