package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validYAML() string {
	return `
server:
  http_addr: ":8080"
  boot_http_addr: ":8082"
  grpc_addr: ":9090"
  external_boot_url: "http://192.0.2.10:8082"
database:
  dsn: "postgres://netboot:pw@localhost:5432/netboot"
valkey:
  addr: "localhost:6379"
artifacts:
  root: "/var/lib/netboot/artifacts"
  max_upload_bytes: 4294967296
netboot:
  dhcp_interface: "eth1"
  tftp_addr: ":69"
  stale_session_timeout: "60m"
bootstrap_operator:
  username: "admin"
  password: "change-me-please"
`
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "conf.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	c, err := Load(writeTemp(t, validYAML()))
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
	if c.Server.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q", c.Server.HTTPAddr)
	}
	if c.Netboot.StaleSessionTimeout.Duration().Minutes() != 60 {
		t.Errorf("stale timeout = %v", c.Netboot.StaleSessionTimeout)
	}
	if c.Artifacts.MaxUploadBytes != 4294967296 {
		t.Errorf("max upload = %d", c.Artifacts.MaxUploadBytes)
	}
}

func TestLoadMissingRequiredFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(string) string
		errSub string
	}{
		{"missing dsn", func(s string) string {
			return strings.Replace(s, `dsn: "postgres://netboot:pw@localhost:5432/netboot"`, `dsn: ""`, 1)
		}, "database.dsn"},
		{"missing valkey", func(s string) string { return strings.Replace(s, `addr: "localhost:6379"`, `addr: ""`, 1) }, "valkey.addr"},
		{"missing artifact root", func(s string) string { return strings.Replace(s, `root: "/var/lib/netboot/artifacts"`, `root: ""`, 1) }, "artifacts.root"},
		{"missing external boot url", func(s string) string {
			return strings.Replace(s, `external_boot_url: "http://192.0.2.10:8082"`, `external_boot_url: ""`, 1)
		}, "server.external_boot_url"},
		{"weak bootstrap password", func(s string) string { return strings.Replace(s, "change-me-please", "short", 1) }, "bootstrap_operator.password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeTemp(t, tc.mutate(validYAML())))
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.errSub)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	yaml := strings.Replace(validYAML(), `stale_session_timeout: "60m"`, "", 1)
	c, err := Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Netboot.StaleSessionTimeout.Duration() != DefaultStaleSessionTimeout {
		t.Errorf("default stale timeout not applied: %v", c.Netboot.StaleSessionTimeout)
	}
	if c.Events.RetentionDays != DefaultEventRetentionDays {
		t.Errorf("default retention not applied: %d", c.Events.RetentionDays)
	}
}

func TestLoadUnreadableFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing file")
	}
}
