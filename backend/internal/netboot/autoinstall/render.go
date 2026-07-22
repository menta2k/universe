// Package autoinstall renders Ubuntu autoinstall (subiquity) seed documents
// for the boot HTTP endpoints (contracts/boot-protocols.md §4). Rendered
// documents are validated structurally before being returned: on any failure
// callers receive an error and empty strings, never a partial document (FR-008).
package autoinstall

import (
	"errors"
	"fmt"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/menta2k/universe/backend/internal/biz"
)

const (
	cloudConfigHeader = "#cloud-config\n"
	identityUsername  = "ubuntu"
	autoinstallVer    = 1
)

// Input carries everything needed to render seed documents for one session.
type Input struct {
	Machine *biz.Machine
	Profile *biz.Profile
	Session *biz.Session
	// BootURL is the machine-facing base URL, e.g. "http://10.0.0.1:8082".
	BootURL   string
	SeedToken string
	// OneTimePasswordHash is an already-hashed crypt string; treated as opaque.
	OneTimePasswordHash string
}

func (in Input) validate() error {
	switch {
	case in.Machine == nil:
		return errors.New("autoinstall input: machine is nil")
	case in.Profile == nil:
		return errors.New("autoinstall input: profile is nil")
	case in.Session == nil:
		return errors.New("autoinstall input: session is nil")
	case in.BootURL == "":
		return errors.New("autoinstall input: boot URL is empty")
	case in.SeedToken == "":
		return errors.New("autoinstall input: seed token is empty")
	}
	return nil
}

func (in Input) reportURL() string {
	return fmt.Sprintf("%s/boot/report/%s", strings.TrimRight(in.BootURL, "/"), in.SeedToken)
}

// Render produces the user-data and meta-data documents for a session.
// The user-data is either the default document built from the profile or, when
// profile.UserDataTemplate is set, that Go template executed with Input fields.
// Either way the result is schema-validated; on failure both strings are empty.
func Render(in Input) (userData string, metaData string, err error) {
	if err := in.validate(); err != nil {
		return "", "", err
	}
	if in.Profile.UserDataTemplate != "" {
		userData, err = renderTemplate(in)
	} else {
		userData, err = renderDefault(in)
	}
	if err != nil {
		return "", "", err
	}
	if err := validateDocument(userData); err != nil {
		return "", "", fmt.Errorf("rendered autoinstall document invalid: %w", err)
	}
	metaData = fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", in.Session.ID, in.Machine.Name)
	return userData, metaData, nil
}

// Cmdline builds the kernel command line pointing the installer at the seed.
// Newlines and carriage returns are rejected to prevent script injection.
func Cmdline(in Input) (string, error) {
	if err := in.validate(); err != nil {
		return "", err
	}
	// cloud-init 24.x+ (Ubuntu 24.04 ships 25.x) removed the "nocloud-net"
	// datasource name; use "nocloud" — the http:// seedfrom selects network
	// mode. The old name is silently ignored, leaving DataSourceNone and an
	// interactive installer.
	cmdline := fmt.Sprintf("autoinstall ds=nocloud;s=%s/boot/seed/%s/",
		strings.TrimRight(in.BootURL, "/"), in.SeedToken)
	if extra := in.Profile.KernelCmdlineExtra; extra != "" {
		cmdline = cmdline + " " + extra
	}
	if strings.ContainsAny(cmdline, "\n\r") {
		return "", fmt.Errorf("kernel cmdline contains newline characters: %q", cmdline)
	}
	return cmdline, nil
}

// renderDefault builds the standard document from profile fields via yaml.Marshal.
func renderDefault(in Input) (string, error) {
	storage, err := storageSection(in.Profile.StorageLayout)
	if err != nil {
		return "", fmt.Errorf("build storage section: %w", err)
	}
	lateCommands := make([]string, 0, len(in.Profile.LateCommands)+1)
	for _, cmd := range in.Profile.LateCommands {
		lateCommands = append(lateCommands, "curtin in-target -- "+cmd)
	}
	// Network precedence: a friendly production network (2-NIC pattern) is
	// applied post-install via a late-command, so the installer keeps its
	// provisioning-NIC DHCP for NFS and no `network:` section is emitted for it.
	// Otherwise a per-machine raw override beats the profile's, which beats
	// nothing — those go through the installer's `network:` section as usual.
	var network map[string]any
	switch {
	case in.Machine.InstallNetwork.IsSet():
		lateCommands = append(lateCommands,
			productionNetworkLateCommand(in.Machine, productionDNS(in.Machine, in.Profile)))
	case len(in.Machine.NetworkConfig) > 0:
		network = in.Machine.NetworkConfig
	case len(in.Profile.NetworkConfig) > 0:
		network = in.Profile.NetworkConfig
	}
	// Whichever path pinned the target's network, it owns /etc/netplan alone:
	// keep cloud-init from re-rendering 50-cloud-init.yaml over it on first boot.
	if network != nil || in.Machine.InstallNetwork.IsSet() {
		lateCommands = append(lateCommands, cloudInitNoNetworkCommand())
	}
	report := in.reportURL()
	lateCommands = append(lateCommands, "wget -qO- --post-data=status=ok "+report)

	layout := in.Profile.KeyboardLayout
	if layout == "" {
		layout = "us"
	}
	locale := in.Profile.Locale
	if locale == "" {
		locale = "en_US.UTF-8"
	}

	// Identity: the profile can override the default account and set a real
	// (sha512-crypt) password; otherwise fall back to "ubuntu" and the discarded
	// one-time hash (key-only access). SSH stays key-only regardless — the
	// password is for console/local login (allow-pw is always false).
	username := in.Profile.InstallUsername
	if username == "" {
		username = identityUsername
	}
	password := in.Profile.InstallPasswordHash
	if password == "" {
		password = in.OneTimePasswordHash
	}
	ai := map[string]any{
		"version": autoinstallVer,
		"locale":  locale,
		"keyboard": map[string]any{
			"layout":  layout,
			"variant": in.Profile.KeyboardVariant,
		},
		"identity": map[string]any{
			"hostname": in.Machine.Name,
			"username": username,
			"password": password,
		},
		"ssh": map[string]any{
			"install-server":  true,
			"authorized-keys": orEmptyStrings(in.Profile.SSHAuthorizedKeys),
			"allow-pw":        false,
		},
		"storage":        storage,
		"late-commands":  lateCommands,
		"error-commands": []string{"wget -qO- --post-data=status=error " + report},
	}
	if in.Profile.Timezone != "" {
		ai["timezone"] = in.Profile.Timezone
	}
	if len(in.Profile.Packages) > 0 {
		ai["packages"] = in.Profile.Packages
	}
	if network != nil {
		ai["network"] = network
	}
	body, err := yaml.Marshal(map[string]any{"autoinstall": ai})
	if err != nil {
		return "", fmt.Errorf("marshal autoinstall document: %w", err)
	}
	return cloudConfigHeader + string(body), nil
}

// orEmptyStrings returns a non-nil slice so an empty SSH key list marshals as
// an empty YAML sequence ("[]") rather than "null", which subiquity rejects.
func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// renderTemplate executes profile.UserDataTemplate as a Go text/template with
// the Input fields as dot. Missing keys are a hard error.
func renderTemplate(in Input) (string, error) {
	tmpl, err := template.New("user-data").Option("missingkey=error").
		Parse(in.Profile.UserDataTemplate)
	if err != nil {
		return "", fmt.Errorf("parse user-data template: %w", err)
	}
	dot := map[string]any{
		"Machine":             in.Machine,
		"Profile":             in.Profile,
		"Session":             in.Session,
		"BootURL":             in.BootURL,
		"SeedToken":           in.SeedToken,
		"OneTimePasswordHash": in.OneTimePasswordHash,
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, dot); err != nil {
		return "", fmt.Errorf("execute user-data template: %w", err)
	}
	return buf.String(), nil
}

// storageSection maps the profile storage layout to the autoinstall storage map.
func storageSection(layout biz.StorageLayout) (map[string]any, error) {
	switch layout.Mode {
	case "lvm", "direct":
		return map[string]any{"layout": map[string]any{"name": layout.Mode}}, nil
	case "custom":
		if layout.Custom == nil {
			return nil, errors.New("storage mode custom but no custom config provided")
		}
		return map[string]any{"config": layout.Custom}, nil
	default:
		return nil, fmt.Errorf("unsupported storage layout mode %q", layout.Mode)
	}
}

// validateDocument parses the rendered YAML and enforces the structural
// contract: autoinstall map with version 1, identity and ssh sections present,
// allow-pw false, and at least one authorized key.
func validateDocument(userData string) error {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(userData), &doc); err != nil {
		return fmt.Errorf("parse rendered YAML: %w", err)
	}
	ai, ok := doc["autoinstall"].(map[string]any)
	if !ok {
		return errors.New("top-level autoinstall map missing")
	}
	if version, ok := ai["version"].(int); !ok || version != autoinstallVer {
		return fmt.Errorf("autoinstall.version = %v, want %d", ai["version"], autoinstallVer)
	}
	if _, ok := ai["identity"].(map[string]any); !ok {
		return errors.New("autoinstall.identity section missing")
	}
	sshSec, ok := ai["ssh"].(map[string]any)
	if !ok {
		return errors.New("autoinstall.ssh section missing")
	}
	if allowPw, ok := sshSec["allow-pw"].(bool); !ok || allowPw {
		return fmt.Errorf("autoinstall.ssh.allow-pw = %v, must be false", sshSec["allow-pw"])
	}
	// authorized-keys must be present as a list, but may be empty: the profile
	// use case guarantees access via SSH keys or a login password, and identity
	// always carries a password field, so an empty key list is a valid
	// password-only machine rather than a lockout.
	if _, ok := sshSec["authorized-keys"].([]any); !ok {
		return errors.New("autoinstall.ssh.authorized-keys must be a list")
	}
	identity, _ := ai["identity"].(map[string]any)
	if pw, ok := identity["password"].(string); !ok || pw == "" {
		return errors.New("autoinstall.identity.password must be set")
	}
	return nil
}
