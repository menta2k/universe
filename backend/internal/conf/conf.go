// Package conf loads and validates the netbootd configuration file.
// Validation is fail-fast: the process must not start with an incomplete
// or insecure configuration (Constitution: Secure by Default).
package conf

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultStaleSessionTimeout = 60 * time.Minute
	DefaultEventRetentionDays  = 90
	DefaultSeedTokenTTL        = 30 * time.Minute
	DefaultLeaseTTL            = time.Hour
	minBootstrapPasswordLen    = 12
)

// Duration wraps time.Duration for YAML string parsing ("60m").
type Duration string

// Duration parses the value; Load has already validated it.
func (d Duration) Duration() time.Duration {
	v, _ := time.ParseDuration(string(d))
	return v
}

type Server struct {
	HTTPAddr string `yaml:"http_addr"`
	GRPCAddr string `yaml:"grpc_addr"`
	// BootHTTPAddr is the machine-facing boot service listen address.
	BootHTTPAddr string `yaml:"boot_http_addr"`
	// ExternalBootURL is the URL booting machines use to reach the boot
	// service (embedded in DHCP options and iPXE scripts).
	ExternalBootURL string `yaml:"external_boot_url"`
}

type Database struct {
	DSN string `yaml:"dsn"`
}

type Valkey struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
}

type Artifacts struct {
	Root           string `yaml:"root"`
	MaxUploadBytes int64  `yaml:"max_upload_bytes"`
}

type Netboot struct {
	DHCPInterface       string   `yaml:"dhcp_interface"`
	DHCPAddr            string   `yaml:"dhcp_addr"`
	TFTPAddr            string   `yaml:"tftp_addr"`
	StaleSessionTimeout Duration `yaml:"stale_session_timeout"`
	SeedTokenTTL        Duration `yaml:"seed_token_ttl"`
	LeaseTTL            Duration `yaml:"lease_ttl"`
}

type Events struct {
	RetentionDays int `yaml:"retention_days"`
}

type BootstrapOperator struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Config struct {
	Server            Server            `yaml:"server"`
	Database          Database          `yaml:"database"`
	Valkey            Valkey            `yaml:"valkey"`
	Artifacts         Artifacts         `yaml:"artifacts"`
	Netboot           Netboot           `yaml:"netboot"`
	Events            Events            `yaml:"events"`
	BootstrapOperator BootstrapOperator `yaml:"bootstrap_operator"`
}

// Load reads, defaults, and validates the configuration at path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is operator-supplied by design
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&c)
	if err := validate(&c); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &c, nil
}

func applyDefaults(c *Config) {
	if c.Server.HTTPAddr == "" {
		c.Server.HTTPAddr = ":8080"
	}
	if c.Server.GRPCAddr == "" {
		c.Server.GRPCAddr = ":9090"
	}
	if c.Server.BootHTTPAddr == "" {
		c.Server.BootHTTPAddr = ":8082"
	}
	if c.Netboot.DHCPAddr == "" {
		c.Netboot.DHCPAddr = ":67"
	}
	if c.Netboot.TFTPAddr == "" {
		c.Netboot.TFTPAddr = ":69"
	}
	if c.Netboot.StaleSessionTimeout == "" {
		c.Netboot.StaleSessionTimeout = Duration(DefaultStaleSessionTimeout.String())
	}
	if c.Netboot.SeedTokenTTL == "" {
		c.Netboot.SeedTokenTTL = Duration(DefaultSeedTokenTTL.String())
	}
	if c.Netboot.LeaseTTL == "" {
		c.Netboot.LeaseTTL = Duration(DefaultLeaseTTL.String())
	}
	if c.Events.RetentionDays == 0 {
		c.Events.RetentionDays = DefaultEventRetentionDays
	}
	if c.Artifacts.MaxUploadBytes == 0 {
		c.Artifacts.MaxUploadBytes = 4 << 30 // 4 GiB
	}
}

func validate(c *Config) error {
	var errs []error
	req := func(field, value string) {
		if value == "" {
			errs = append(errs, fmt.Errorf("%s is required", field))
		}
	}
	req("database.dsn", c.Database.DSN)
	req("valkey.addr", c.Valkey.Addr)
	req("artifacts.root", c.Artifacts.Root)
	req("server.external_boot_url", c.Server.ExternalBootURL)
	req("netboot.dhcp_interface", c.Netboot.DHCPInterface)
	req("bootstrap_operator.username", c.BootstrapOperator.Username)

	for field, d := range map[string]Duration{
		"netboot.stale_session_timeout": c.Netboot.StaleSessionTimeout,
		"netboot.seed_token_ttl":        c.Netboot.SeedTokenTTL,
		"netboot.lease_ttl":             c.Netboot.LeaseTTL,
	} {
		if _, err := time.ParseDuration(string(d)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", field, err))
		}
	}
	if len(c.BootstrapOperator.Password) < minBootstrapPasswordLen {
		errs = append(errs, fmt.Errorf(
			"bootstrap_operator.password must be at least %d characters", minBootstrapPasswordLen))
	}
	return errors.Join(errs...)
}
