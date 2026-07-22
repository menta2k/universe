package autoinstall

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
)

var b64InCmd = regexp.MustCompile(`printf %s (\S+) \| base64 -d`)

// assertCloudInitNetworkDisabled checks the late-commands carry the drop-in that
// stops cloud-init re-rendering 50-cloud-init.yaml over the pinned netplan, and
// that the drop-in really disables networking.
func assertCloudInitNetworkDisabled(t *testing.T, late []any) {
	t.Helper()
	for _, c := range late {
		s, _ := c.(string)
		if !strings.Contains(s, cloudInitNoNetworkPath) {
			continue
		}
		m := b64InCmd.FindStringSubmatch(s)
		if m == nil {
			t.Fatalf("no base64 drop-in body in command: %s", s)
		}
		raw, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil {
			t.Fatalf("decode drop-in base64: %v", err)
		}
		if !strings.Contains(string(raw), "config: disabled") {
			t.Errorf("drop-in must disable cloud-init networking, got: %q", raw)
		}
		return
	}
	t.Errorf("no cloud-init network drop-in; 50-cloud-init.yaml will override the pinned netplan: %v", late)
}

func TestRenderProductionNetwork(t *testing.T) {
	in := defaultInput()
	in.Profile.DefaultDNS = []string{"1.1.1.1", "8.8.8.8"} // machine has none -> inherits these
	in.Machine.MAC = "BC:24:11:AA:BB:CC"                   // uppercase: must be lowercased in the command
	in.Machine.InstallNetwork.Address = "10.20.0.10/24"
	in.Machine.InstallNetwork.Gateway = "10.20.0.1"

	userData, _, err := Render(in)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	ai := parseUserData(t, userData)

	// The installer network is left alone (no `network:` section) so the
	// provisioning NIC keeps its DHCP lease for NFS.
	if _, present := ai["network"]; present {
		t.Errorf("production network must not emit a `network:` section (breaks NFS): %v", ai["network"])
	}

	late, _ := ai["late-commands"].([]any)
	var cmd string
	for _, c := range late {
		if s, _ := c.(string); strings.Contains(s, "install") || strings.Contains(s, "netplan") {
			if strings.Contains(s, "base64 -d") {
				cmd = s
			}
		}
	}
	if cmd == "" {
		t.Fatalf("no production-network late-command found in: %v", late)
	}
	if !strings.Contains(cmd, "PROV=bc:24:11:aa:bb:cc") {
		t.Errorf("provisioning MAC must be lowercased in the command: %s", cmd)
	}
	if strings.Contains(cmd, "'") && strings.Count(cmd, "'") != 2 {
		t.Errorf("command must be single-quote-safe (exactly the wrapping quotes): %s", cmd)
	}

	// 00-netbootd.yaml must be the only netplan file: netplan applies in lexical
	// order, so a cloud-init-rendered 50-cloud-init.yaml would override it.
	assertCloudInitNetworkDisabled(t, late)
	if strings.Contains(cmd, "50-cloud-init.yaml") {
		t.Errorf("must not write a second netplan file that fights 00-netbootd.yaml: %s", cmd)
	}

	m := b64InCmd.FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("no base64 netplan in command: %s", cmd)
	}
	raw, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		t.Fatalf("decode netplan base64: %v", err)
	}
	netplan := string(raw)
	for _, want := range []string{
		"__PROD_MAC__",                          // production NIC filled in at runtime
		"10.20.0.10/24",                         // static address
		"to: default",                           // default route on the production NIC
		"via: \"10.20.0.1\"",                    // gateway
		"addresses: [\"1.1.1.1\", \"8.8.8.8\"]", // DNS inherited from profile default
		"macaddress: \"bc:24:11:aa:bb:cc\"",     // provisioning NIC matched
		"activation-mode: \"off\"",              // provisioning NIC taken down
	} {
		if !strings.Contains(netplan, want) {
			t.Errorf("netplan missing %q:\n%s", want, netplan)
		}
	}
}
