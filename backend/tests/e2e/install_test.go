//go:build e2e

// Package e2e drives a real unattended Ubuntu install in a QEMU VM against a
// running netbootd. It is guarded by the `e2e` build tag and requires KVM,
// qemu-system-x86_64, ovmf, and CAP_NET_ADMIN to create the test bridge.
// Run with: make test-e2e
package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestUnattendedInstall boots a VM in the given firmware mode and asserts the
// machine reports install completion within the deadline (quickstart Scenario 1).
//
// This test is a scaffold: it shells out to the harness scripts and polls the
// admin API for the machine to reach state "installed". Wiring the config,
// artifact upload, profile, and machine registration is done via the API before
// booting — see the helper stubs below, which must be filled in for the target
// environment (they depend on site-specific artifact locations).
func TestUnattendedInstall(t *testing.T) {
	for _, fw := range []string{"bios", "uefi"} {
		t.Run(fw, func(t *testing.T) {
			requireTools(t, "qemu-system-x86_64", "qemu-img")
			if os.Getenv("NBTEST_READY") == "" {
				t.Skip("set NBTEST_READY=1 with a provisioning bridge + netbootd running")
			}

			mac := "52:54:00:e2:e2:01"
			// Precondition: machine registered + provisioned via API (see quickstart).
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()

			boot := exec.CommandContext(ctx, scriptPath(t, "boot-vm.sh"),
				"--mac", mac, "--firmware", fw)
			boot.Stdout, boot.Stderr = os.Stderr, os.Stderr
			if err := boot.Start(); err != nil {
				t.Fatalf("start vm: %v", err)
			}
			t.Cleanup(func() { _ = boot.Process.Kill() })

			if err := waitForInstalled(ctx, mac); err != nil {
				t.Fatalf("machine %s did not reach installed: %v", mac, err)
			}
		})
	}
}

func requireTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("required tool %q not found", tool)
		}
	}
}

func scriptPath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("scripts", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// waitForInstalled polls the admin API until the machine's provision_state is
// "installed" or ctx expires. Implemented against the site's API base URL from
// the NBTEST_API env var.
func waitForInstalled(ctx context.Context, mac string) error {
	// Implementation depends on NBTEST_API + operator credentials; left as a
	// site hook so CI can inject its endpoint. Polls GET /api/v1/machines?q=<mac>.
	_ = mac
	<-ctx.Done()
	return ctx.Err()
}
