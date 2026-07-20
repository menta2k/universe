package autoinstall

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/menta2k/universe/backend/internal/biz"
)

// defaultInput builds a fully-populated Input; tests mutate copies of it.
func defaultInput() Input {
	return Input{
		Machine: &biz.Machine{ID: "machine-1", MAC: "aa:bb:cc:dd:ee:ff", Name: "worker-01"},
		Profile: &biz.Profile{
			ID:                "profile-1",
			Name:              "base",
			Version:           3,
			StorageLayout:     biz.StorageLayout{Mode: "lvm"},
			KeyboardLayout:    "gb",
			Locale:            "en_GB.UTF-8",
			Timezone:          "Europe/London",
			Packages:          []string{"curl", "openssh-server"},
			SSHAuthorizedKeys: []string{"ssh-ed25519 AAAA key1", "ssh-rsa BBBB key2"},
			LateCommands:      []string{"systemctl enable foo", "touch /done"},
		},
		Session:             &biz.Session{ID: "sess-42", MachineID: "machine-1", ProfileID: "profile-1"},
		BootURL:             "http://10.0.0.1:8082",
		SeedToken:           "tok123",
		OneTimePasswordHash: "$6$rounds=4096$salt$hashedpw",
	}
}

// parseUserData asserts the #cloud-config header and returns the autoinstall map.
func parseUserData(t *testing.T, userData string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(userData, "#cloud-config\n") {
		t.Fatalf("user-data missing #cloud-config header: %q", userData)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(userData), &doc); err != nil {
		t.Fatalf("user-data is not valid YAML: %v", err)
	}
	ai, ok := doc["autoinstall"].(map[string]any)
	if !ok {
		t.Fatalf("top-level autoinstall map missing: %v", doc)
	}
	return ai
}

func TestRenderDefaultHappyPath(t *testing.T) {
	in := defaultInput()
	userData, metaData, err := Render(in)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	ai := parseUserData(t, userData)

	if v, _ := ai["version"].(int); v != 1 {
		t.Errorf("version = %v, want 1", ai["version"])
	}

	identity, _ := ai["identity"].(map[string]any)
	if identity == nil {
		t.Fatal("identity section missing")
	}
	if identity["hostname"] != "worker-01" {
		t.Errorf("identity.hostname = %v, want worker-01", identity["hostname"])
	}
	if identity["username"] != "ubuntu" {
		t.Errorf("identity.username = %v, want ubuntu", identity["username"])
	}
	if identity["password"] != in.OneTimePasswordHash {
		t.Errorf("identity.password = %v, want %v", identity["password"], in.OneTimePasswordHash)
	}

	sshSec, _ := ai["ssh"].(map[string]any)
	if sshSec == nil {
		t.Fatal("ssh section missing")
	}
	if sshSec["install-server"] != true {
		t.Errorf("ssh.install-server = %v, want true", sshSec["install-server"])
	}
	if sshSec["allow-pw"] != false {
		t.Errorf("ssh.allow-pw = %v, want false", sshSec["allow-pw"])
	}
	keys, _ := sshSec["authorized-keys"].([]any)
	if len(keys) != 2 || keys[0] != "ssh-ed25519 AAAA key1" {
		t.Errorf("ssh.authorized-keys = %v, want profile keys", keys)
	}

	storage, _ := ai["storage"].(map[string]any)
	if storage == nil {
		t.Fatal("storage section missing")
	}
	layout, _ := storage["layout"].(map[string]any)
	if layout == nil || layout["name"] != "lvm" {
		t.Errorf("storage.layout = %v, want {name: lvm}", storage["layout"])
	}

	pkgs, _ := ai["packages"].([]any)
	if len(pkgs) != 2 || pkgs[0] != "curl" || pkgs[1] != "openssh-server" {
		t.Errorf("packages = %v, want [curl openssh-server]", pkgs)
	}

	if _, present := ai["network"]; present {
		t.Errorf("network should be omitted when profile.NetworkConfig is empty")
	}

	if ai["locale"] != "en_GB.UTF-8" {
		t.Errorf("locale = %v, want en_GB.UTF-8", ai["locale"])
	}
	if ai["timezone"] != "Europe/London" {
		t.Errorf("timezone = %v, want Europe/London", ai["timezone"])
	}
	keyboard, _ := ai["keyboard"].(map[string]any)
	if keyboard == nil || keyboard["layout"] != "gb" {
		t.Errorf("keyboard = %v, want {layout: gb}", ai["keyboard"])
	}

	wantLate := []any{
		"curtin in-target -- systemctl enable foo",
		"curtin in-target -- touch /done",
		"wget -qO- --post-data=status=ok http://10.0.0.1:8082/boot/report/tok123",
	}
	late, _ := ai["late-commands"].([]any)
	if fmt.Sprint(late) != fmt.Sprint(wantLate) {
		t.Errorf("late-commands = %v, want %v", late, wantLate)
	}

	errCmds, _ := ai["error-commands"].([]any)
	wantErr := "wget -qO- --post-data=status=error http://10.0.0.1:8082/boot/report/tok123"
	if len(errCmds) != 1 || errCmds[0] != wantErr {
		t.Errorf("error-commands = %v, want [%q]", errCmds, wantErr)
	}

	wantMeta := "instance-id: sess-42\nlocal-hostname: worker-01\n"
	if metaData != wantMeta {
		t.Errorf("meta-data = %q, want %q", metaData, wantMeta)
	}
}

func TestRenderStorageModes(t *testing.T) {
	tests := []struct {
		name   string
		layout biz.StorageLayout
		check  func(t *testing.T, storage map[string]any)
	}{
		{
			name:   "lvm",
			layout: biz.StorageLayout{Mode: "lvm"},
			check: func(t *testing.T, storage map[string]any) {
				layout, _ := storage["layout"].(map[string]any)
				if layout == nil || layout["name"] != "lvm" {
					t.Errorf("storage = %v, want layout {name: lvm}", storage)
				}
			},
		},
		{
			name:   "direct",
			layout: biz.StorageLayout{Mode: "direct"},
			check: func(t *testing.T, storage map[string]any) {
				layout, _ := storage["layout"].(map[string]any)
				if layout == nil || layout["name"] != "direct" {
					t.Errorf("storage = %v, want layout {name: direct}", storage)
				}
			},
		},
		{
			name: "custom",
			layout: biz.StorageLayout{
				Mode: "custom",
				Custom: []any{
					map[string]any{"type": "disk", "id": "disk0", "ptable": "gpt"},
				},
			},
			check: func(t *testing.T, storage map[string]any) {
				cfg, _ := storage["config"].([]any)
				if len(cfg) != 1 {
					t.Fatalf("storage.config = %v, want 1 entry", storage["config"])
				}
				entry, _ := cfg[0].(map[string]any)
				if entry["type"] != "disk" || entry["id"] != "disk0" || entry["ptable"] != "gpt" {
					t.Errorf("storage.config[0] = %v, want custom entry verbatim", entry)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := defaultInput()
			in.Profile.StorageLayout = tt.layout
			userData, _, err := Render(in)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			ai := parseUserData(t, userData)
			storage, _ := ai["storage"].(map[string]any)
			if storage == nil {
				t.Fatal("storage section missing")
			}
			tt.check(t, storage)
		})
	}
}

func TestRenderUnknownStorageMode(t *testing.T) {
	in := defaultInput()
	in.Profile.StorageLayout = biz.StorageLayout{Mode: "zfs-magic"}
	userData, metaData, err := Render(in)
	if err == nil {
		t.Fatal("Render() with unknown storage mode: want error, got nil")
	}
	if userData != "" || metaData != "" {
		t.Errorf("want empty outputs on error, got userData=%q metaData=%q", userData, metaData)
	}
}

func TestRenderNetworkConfigPassthrough(t *testing.T) {
	in := defaultInput()
	in.Profile.NetworkConfig = map[string]any{
		"version": 2,
		"ethernets": map[string]any{
			"eth0": map[string]any{"dhcp4": true},
		},
	}
	userData, _, err := Render(in)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	ai := parseUserData(t, userData)
	network, _ := ai["network"].(map[string]any)
	if network == nil {
		t.Fatal("network section missing")
	}
	if v, _ := network["version"].(int); v != 2 {
		t.Errorf("network.version = %v, want 2", network["version"])
	}
	eth, _ := network["ethernets"].(map[string]any)
	eth0, _ := eth["eth0"].(map[string]any)
	if eth0 == nil || eth0["dhcp4"] != true {
		t.Errorf("network.ethernets.eth0 = %v, want {dhcp4: true}", eth0)
	}
}

func TestRenderEmptySSHKeysFails(t *testing.T) {
	in := defaultInput()
	in.Profile.SSHAuthorizedKeys = nil
	userData, metaData, err := Render(in)
	if err == nil {
		t.Fatal("Render() with no SSH keys: want error, got nil")
	}
	if userData != "" || metaData != "" {
		t.Errorf("want empty outputs on error, got userData=%q metaData=%q", userData, metaData)
	}
}

func TestRenderCustomTemplate(t *testing.T) {
	in := defaultInput()
	in.Profile.UserDataTemplate = `#cloud-config
autoinstall:
  version: 1
  identity:
    hostname: {{ .Machine.Name }}
    username: ubuntu
    password: "{{ .OneTimePasswordHash }}"
  ssh:
    install-server: true
    allow-pw: false
    authorized-keys:
{{- range .Profile.SSHAuthorizedKeys }}
      - "{{ . }}"
{{- end }}
  late-commands:
    - wget -qO- --post-data=status=ok {{ .BootURL }}/boot/report/{{ .SeedToken }}
`
	userData, metaData, err := Render(in)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	ai := parseUserData(t, userData)
	identity, _ := ai["identity"].(map[string]any)
	if identity["hostname"] != "worker-01" {
		t.Errorf("template identity.hostname = %v, want worker-01", identity["hostname"])
	}
	if identity["password"] != in.OneTimePasswordHash {
		t.Errorf("template identity.password = %v, want hash", identity["password"])
	}
	late, _ := ai["late-commands"].([]any)
	want := "wget -qO- --post-data=status=ok http://10.0.0.1:8082/boot/report/tok123"
	if len(late) != 1 || late[0] != want {
		t.Errorf("template late-commands = %v, want [%q]", late, want)
	}
	if !strings.Contains(metaData, "instance-id: sess-42") {
		t.Errorf("meta-data = %q, want instance-id present", metaData)
	}
}

func TestRenderCustomTemplateFailures(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
	}{
		{
			name: "missing key",
			tmpl: "#cloud-config\nautoinstall:\n  version: 1\n  identity:\n    hostname: {{ .NoSuchField }}\n",
		},
		{
			// Valid template, but output violates the contract (allow-pw true).
			name: "output violates contract",
			tmpl: "#cloud-config\nautoinstall:\n  version: 1\n" +
				"  identity: {hostname: {{ .Machine.Name }}, username: ubuntu, password: x}\n" +
				"  ssh: {install-server: true, allow-pw: true, authorized-keys: [k1]}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := defaultInput()
			in.Profile.UserDataTemplate = tt.tmpl
			userData, metaData, err := Render(in)
			if err == nil {
				t.Fatal("Render() want error, got nil")
			}
			if userData != "" || metaData != "" {
				t.Errorf("want empty outputs on error, got userData=%q metaData=%q", userData, metaData)
			}
		})
	}
}

func TestRenderNilInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Input)
	}{
		{"nil machine", func(in *Input) { in.Machine = nil }},
		{"nil profile", func(in *Input) { in.Profile = nil }},
		{"nil session", func(in *Input) { in.Session = nil }},
		{"empty boot url", func(in *Input) { in.BootURL = "" }},
		{"empty seed token", func(in *Input) { in.SeedToken = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := defaultInput()
			tt.mutate(&in)
			userData, metaData, err := Render(in)
			if err == nil {
				t.Fatal("Render() want error, got nil")
			}
			if userData != "" || metaData != "" {
				t.Errorf("want empty outputs on error")
			}
		})
	}
}

func TestCmdline(t *testing.T) {
	tests := []struct {
		name    string
		extra   string
		want    string
		wantErr bool
	}{
		{
			name:  "default",
			extra: "",
			want:  "autoinstall ds=nocloud;s=http://10.0.0.1:8082/boot/seed/tok123/",
		},
		{
			name:  "with extra",
			extra: "console=ttyS0 quiet",
			want:  "autoinstall ds=nocloud;s=http://10.0.0.1:8082/boot/seed/tok123/ console=ttyS0 quiet",
		},
		{
			name:    "newline injection",
			extra:   "quiet\nmalicious=1",
			wantErr: true,
		},
		{
			name:    "carriage return injection",
			extra:   "quiet\rmalicious=1",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := defaultInput()
			in.Profile.KernelCmdlineExtra = tt.extra
			got, err := Cmdline(in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Cmdline() = %q, want error", got)
				}
				if got != "" {
					t.Errorf("want empty string on error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Cmdline() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Cmdline() = %q, want %q", got, tt.want)
			}
		})
	}
}
