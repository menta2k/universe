package nfs

import (
	"os"
	"strings"
	"testing"
)

func TestIsMountedFalseForOrdinaryDir(t *testing.T) {
	dir := t.TempDir()
	if IsMounted(dir) {
		t.Errorf("IsMounted(%q) = true, want false for a plain directory", dir)
	}
}

func TestIsMountedTrueForRoot(t *testing.T) {
	// "/" is always a mount; exercises the /proc/mounts scan.
	if _, err := os.Stat("/proc/mounts"); err != nil {
		t.Skip("no /proc/mounts")
	}
	if !IsMounted("/") {
		t.Error("IsMounted(/) = false, want true")
	}
}

func TestMountISOCreatesMountpointThenFails(t *testing.T) {
	// Not running as root (CI), so the actual mount must fail — but the
	// mountpoint should be created and the error should name the mount.
	dir := t.TempDir()
	mp := dir + "/noble"
	err := MountISO(dir+"/nonexistent.iso", mp)
	if err == nil {
		t.Skip("mount unexpectedly succeeded (running privileged?)")
	}
	if _, statErr := os.Stat(mp); statErr != nil {
		t.Errorf("mountpoint not created: %v", statErr)
	}
	if !strings.Contains(err.Error(), "mount") {
		t.Errorf("error should mention mount, got: %v", err)
	}
}

func TestUnmountUnmountedIsNoop(t *testing.T) {
	if err := Unmount(t.TempDir()); err != nil {
		t.Errorf("Unmount of a non-mount should be a no-op, got: %v", err)
	}
}
