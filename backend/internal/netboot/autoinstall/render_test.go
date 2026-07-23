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

func TestRenderInstallIdentity(t *testing.T) {
	t.Run("profile username and password override the defaults", func(t *testing.T) {
		in := defaultInput()
		in.Profile.InstallUsername = "operator"
		in.Profile.InstallPasswordHash = "$6$abc$def"
		userData, _, err := Render(in)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		ai := parseUserData(t, userData)
		identity, _ := ai["identity"].(map[string]any)
		if identity["username"] != "operator" {
			t.Errorf("username = %v, want operator", identity["username"])
		}
		if identity["password"] != "$6$abc$def" {
			t.Errorf("password = %v, want profile hash", identity["password"])
		}
	})

	t.Run("defaults fall back to ubuntu and the one-time hash", func(t *testing.T) {
		in := defaultInput() // no InstallUsername/InstallPasswordHash
		userData, _, err := Render(in)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		identity, _ := parseUserData(t, userData)["identity"].(map[string]any)
		if identity["username"] != identityUsername {
			t.Errorf("username = %v, want %s", identity["username"], identityUsername)
		}
		if identity["password"] != in.OneTimePasswordHash {
			t.Errorf("password = %v, want one-time hash", identity["password"])
		}
	})

	t.Run("no SSH keys renders an empty list and stays key-only", func(t *testing.T) {
		in := defaultInput()
		in.Profile.SSHAuthorizedKeys = nil
		in.Profile.InstallPasswordHash = "$6$abc$def"
		userData, _, err := Render(in)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		sshSec, _ := parseUserData(t, userData)["ssh"].(map[string]any)
		keys, ok := sshSec["authorized-keys"].([]any)
		if !ok || len(keys) != 0 {
			t.Errorf("authorized-keys = %v, want empty list", sshSec["authorized-keys"])
		}
		if sshSec["allow-pw"] != false {
			t.Errorf("allow-pw = %v, want false (SSH stays key-only)", sshSec["allow-pw"])
		}
	})
}

func TestRenderMachineNetworkOverride(t *testing.T) {
	profileNet := map[string]any{"version": 2, "ethernets": map[string]any{"eth0": map[string]any{"dhcp4": true}}}
	machineNet := map[string]any{"version": 2, "ethernets": map[string]any{"eth0": map[string]any{"addresses": []any{"10.0.0.5/24"}}}}

	t.Run("machine override wins over the profile network", func(t *testing.T) {
		in := defaultInput()
		in.Profile.NetworkConfig = profileNet
		in.Machine.NetworkConfig = machineNet
		ai := parseUserData(t, mustRender(t, in))
		network, _ := ai["network"].(map[string]any)
		eth, _ := network["ethernets"].(map[string]any)
		eth0, _ := eth["eth0"].(map[string]any)
		if _, isDHCP := eth0["dhcp4"]; isDHCP {
			t.Errorf("machine override ignored; got profile network: %v", network)
		}
		if eth0["addresses"] == nil {
			t.Errorf("machine static addresses missing: %v", network)
		}
	})

	t.Run("no machine override falls back to the profile network", func(t *testing.T) {
		in := defaultInput()
		in.Profile.NetworkConfig = profileNet
		ai := parseUserData(t, mustRender(t, in))
		if _, ok := ai["network"].(map[string]any); !ok {
			t.Errorf("profile network should be used when no machine override: %v", ai["network"])
		}
	})

	// A raw override lands in the target as subiquity's 00-installer-config.yaml,
	// which cloud-init's 50-cloud-init.yaml would override on the first boot just
	// as it would the friendly production network's 00-netbootd.yaml.
	t.Run("a raw override also disables cloud-init networking", func(t *testing.T) {
		for name, mutate := range map[string]func(*Input){
			"machine": func(in *Input) { in.Machine.NetworkConfig = machineNet },
			"profile": func(in *Input) { in.Profile.NetworkConfig = profileNet },
		} {
			t.Run(name, func(t *testing.T) {
				in := defaultInput()
				mutate(&in)
				late, _ := parseUserData(t, mustRender(t, in))["late-commands"].([]any)
				assertCloudInitNetworkDisabled(t, late)
			})
		}
	})

	t.Run("no network config leaves cloud-init alone", func(t *testing.T) {
		late, _ := parseUserData(t, mustRender(t, defaultInput()))["late-commands"].([]any)
		for _, c := range late {
			if s, _ := c.(string); strings.Contains(s, cloudInitNoNetworkPath) {
				t.Errorf("must not disable cloud-init networking when nothing pins the network: %s", s)
			}
		}
	})
}

func mustRender(t *testing.T, in Input) string {
	t.Helper()
	userData, _, err := Render(in)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return userData
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

// TestRenderEmptySSHKeysSucceeds documents that the render layer no longer
// requires SSH keys: a machine may authenticate by password instead. The
// "keys or password" access policy is enforced in the biz profile use case, not
// here, and identity always carries a password field (a real one or the
// discarded one-time hash).
func TestRenderEmptySSHKeysSucceeds(t *testing.T) {
	in := defaultInput()
	in.Profile.SSHAuthorizedKeys = nil
	userData, _, err := Render(in)
	if err != nil {
		t.Fatalf("Render() with no SSH keys should succeed, got %v", err)
	}
	sshSec, _ := parseUserData(t, userData)["ssh"].(map[string]any)
	if keys, ok := sshSec["authorized-keys"].([]any); !ok || len(keys) != 0 {
		t.Errorf("authorized-keys = %v, want empty list", sshSec["authorized-keys"])
	}
}

// A profile may leave keyboard/locale unset; the render layer supplies the
// defaults so subiquity never sees an empty string.
func TestRenderKeyboardAndLocaleDefaults(t *testing.T) {
	in := defaultInput()
	in.Profile.KeyboardLayout = ""
	in.Profile.Locale = ""
	ai := parseUserData(t, mustRender(t, in))

	if locale, _ := ai["locale"].(string); locale != "en_US.UTF-8" {
		t.Errorf("locale = %q, want the en_US.UTF-8 default", locale)
	}
	keyboard, _ := ai["keyboard"].(map[string]any)
	if layout, _ := keyboard["layout"].(string); layout != "us" {
		t.Errorf("keyboard.layout = %q, want the us default", layout)
	}
}

// validateDocument is the last gate before a seed reaches a machine (FR-008):
// every rejection below would otherwise become a broken or insecure install.
func TestValidateDocumentRejections(t *testing.T) {
	const goodSSH = "  ssh:\n    install-server: true\n    authorized-keys: []\n    allow-pw: false\n"
	const goodIdentity = "  identity:\n    hostname: h\n    username: u\n    password: \"$6$x$y\"\n"

	cases := map[string]string{
		"not YAML at all":     "#cloud-config\n\tthis: [is: not: yaml",
		"no autoinstall map":  "#cloud-config\nsomething-else: {}\n",
		"wrong version":       "#cloud-config\nautoinstall:\n  version: 2\n" + goodIdentity + goodSSH,
		"no identity section": "#cloud-config\nautoinstall:\n  version: 1\n" + goodSSH,
		"no ssh section":      "#cloud-config\nautoinstall:\n  version: 1\n" + goodIdentity,
		"authorized-keys is not a list": "#cloud-config\nautoinstall:\n  version: 1\n" + goodIdentity +
			"  ssh:\n    authorized-keys: \"ssh-ed25519 AAAA\"\n    allow-pw: false\n",
		"empty password": "#cloud-config\nautoinstall:\n  version: 1\n" +
			"  identity:\n    hostname: h\n    username: u\n    password: \"\"\n" + goodSSH,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateDocument(doc); err == nil {
				t.Errorf("validateDocument() accepted an invalid document:\n%s", doc)
			}
		})
	}
}

func TestCmdlineRejectsIncompleteInput(t *testing.T) {
	// Cmdline validates the same Input as Render, so a caller cannot build a
	// kernel command line pointing at a seed that was never rendered.
	for name, mutate := range map[string]func(*Input){
		"no machine":    func(in *Input) { in.Machine = nil },
		"no profile":    func(in *Input) { in.Profile = nil },
		"no session":    func(in *Input) { in.Session = nil },
		"no boot URL":   func(in *Input) { in.BootURL = "" },
		"no seed token": func(in *Input) { in.SeedToken = "" },
	} {
		t.Run(name, func(t *testing.T) {
			in := defaultInput()
			mutate(&in)
			cmdline, err := Cmdline(in)
			if err == nil {
				t.Errorf("Cmdline() should reject incomplete input, got %q", cmdline)
			}
			if cmdline != "" {
				t.Errorf("Cmdline() must return an empty string on error, got %q", cmdline)
			}
		})
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
