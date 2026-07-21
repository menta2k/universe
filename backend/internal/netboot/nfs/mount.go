package nfs

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MountISO loop-mounts iso read-only at mountpoint using the kernel's iso9660
// driver (which reads Rock Ridge long filenames correctly, unlike userspace Go
// ISO parsers). It is idempotent: an already-mounted point is left as-is.
// Requires the process to have CAP_SYS_ADMIN (netbootd runs as root).
func MountISO(iso, mountpoint string) error {
	if err := os.MkdirAll(mountpoint, 0o755); err != nil {
		return fmt.Errorf("create mountpoint %s: %w", mountpoint, err)
	}
	if IsMounted(mountpoint) {
		return nil
	}
	// #nosec G204 -- iso and mountpoint are daemon-controlled paths, not user input.
	cmd := exec.Command("mount", "-o", "loop,ro,nosuid,nodev", "-t", "iso9660", iso, mountpoint)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mount %s at %s: %w: %s", iso, mountpoint, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Unmount unmounts mountpoint if it is mounted.
func Unmount(mountpoint string) error {
	if !IsMounted(mountpoint) {
		return nil
	}
	// #nosec G204 -- mountpoint is a daemon-controlled path.
	if out, err := exec.Command("umount", mountpoint).CombinedOutput(); err != nil {
		return fmt.Errorf("umount %s: %w: %s", mountpoint, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsMounted reports whether mountpoint is an active mount (via /proc/mounts).
func IsMounted(mountpoint string) bool {
	abs, err := filepath.Abs(mountpoint)
	if err != nil {
		return false
	}
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[1] == abs {
			return true
		}
	}
	_ = sc.Err()
	return false
}
