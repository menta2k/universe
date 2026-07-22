package autoinstall

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/menta2k/universe/backend/internal/biz"
)

const (
	// cloudInitNoNetworkPath is the cloud-init drop-in that stops the installed
	// system from re-rendering /etc/netplan/50-cloud-init.yaml on every boot.
	// The 99- prefix makes it the last cloud.cfg.d fragment merged, so it wins.
	cloudInitNoNetworkPath = "/etc/cloud/cloud.cfg.d/99-netbootd-disable-network.cfg"
	// cloudInitNoNetwork disables only cloud-init's network stage; the rest of
	// cloud-init (users, SSH keys, hostname) still runs on the installed system.
	cloudInitNoNetwork = "network: {config: disabled}\n"
)

// productionNetworkLateCommand renders the friendly "production network" of a
// machine into a curtin late-command that writes the installed system's netplan.
//
// It deliberately does NOT go through the autoinstall `network:` section: on an
// NFS-root install the provisioning NIC must keep its DHCP lease for the whole
// install, so touching the live network would drop the root. Running as a
// late-command means the config only lands in the target and takes effect on the
// first reboot.
//
// The production NIC is detected at install time as the first physical ethernet
// whose MAC differs from the provisioning (boot) NIC — the operator only
// supplies address/gateway/DNS. The provisioning NIC is taken down
// (activation-mode: off) so it holds no IP and contributes no default route; the
// production NIC carries the only default route.
//
// 00-netbootd.yaml is left as the single netplan file; cloudInitNoNetworkCommand
// keeps it that way.
func productionNetworkLateCommand(machine *biz.Machine, dns []string) string {
	netplan := productionNetplan(machine.InstallNetwork, strings.ToLower(machine.MAC), dns)
	b64 := base64.StdEncoding.EncodeToString([]byte(netplan))

	// POSIX sh, single-quote-safe (no ' inside) because it is wrapped as
	// `curtin in-target -- sh -c '<script>'`. Detects the production NIC, then
	// decodes the netplan and substitutes its MAC.
	script := strings.Join([]string{
		"set -e",
		"PROV=" + strings.ToLower(machine.MAC),
		"PROD=",
		"for d in /sys/class/net/*; do " +
			"[ -e \"$d/device\" ] || continue; " +
			"m=$(cat \"$d/address\" 2>/dev/null); " +
			"[ -z \"$m\" ] && continue; " +
			"[ \"$m\" = \"$PROV\" ] && continue; " +
			"PROD=$m; break; done",
		"[ -n \"$PROD\" ] || { echo netbootd:no-production-nic >&2; exit 1; }",
		"rm -f /etc/netplan/*.yaml",
		"printf %s " + b64 + " | base64 -d | sed \"s/__PROD_MAC__/$PROD/\" > /etc/netplan/00-netbootd.yaml",
		"chmod 600 /etc/netplan/00-netbootd.yaml",
	}, "; ")

	return "curtin in-target -- sh -c '" + script + "'"
}

// cloudInitNoNetworkCommand returns a late-command that stops the installed
// system's cloud-init from re-rendering /etc/netplan/50-cloud-init.yaml.
//
// netplan merges /etc/netplan/*.yaml in lexical order, so a 50-cloud-init.yaml
// sorts after — and overrides the ethernets of — both 00-netbootd.yaml (the
// friendly production network) and subiquity's 00-installer-config.yaml (a raw
// network override). cloud-init rebuilds that file on first boot from its own
// datasource view, which is DHCP on whatever NIC it finds, so without this the
// operator's static addressing loses to DHCP on the first reboot. Emitted for
// every path that pins the target's network, leaving exactly one owner.
func cloudInitNoNetworkCommand() string {
	b64 := base64.StdEncoding.EncodeToString([]byte(cloudInitNoNetwork))
	script := strings.Join([]string{
		"set -e",
		"mkdir -p /etc/cloud/cloud.cfg.d",
		"printf %s " + b64 + " | base64 -d > " + cloudInitNoNetworkPath,
	}, "; ")
	return "curtin in-target -- sh -c '" + script + "'"
}

// productionNetplan builds the target netplan YAML with a __PROD_MAC__
// placeholder the late-command fills in once the production NIC is detected.
func productionNetplan(n biz.InstallNetwork, provMAC string, dns []string) string {
	var b strings.Builder
	b.WriteString("network:\n  version: 2\n  ethernets:\n")
	b.WriteString("    prod:\n      match:\n        macaddress: \"__PROD_MAC__\"\n")
	b.WriteString("      set-name: eth0\n")
	fmt.Fprintf(&b, "      addresses: [\"%s\"]\n", n.Address)
	if n.Gateway != "" {
		b.WriteString("      routes:\n        - to: default\n")
		fmt.Fprintf(&b, "          via: \"%s\"\n", n.Gateway)
	}
	if len(dns) > 0 {
		quoted := make([]string, len(dns))
		for i, d := range dns {
			quoted[i] = "\"" + d + "\""
		}
		fmt.Fprintf(&b, "      nameservers:\n        addresses: [%s]\n", strings.Join(quoted, ", "))
	}
	fmt.Fprintf(&b, "    prov:\n      match:\n        macaddress: \"%s\"\n", provMAC)
	b.WriteString("      set-name: eth1\n      activation-mode: \"off\"\n")
	return b.String()
}

// productionDNS returns the machine's DNS if set, else the profile's default.
func productionDNS(machine *biz.Machine, profile *biz.Profile) []string {
	if len(machine.InstallNetwork.DNS) > 0 {
		return machine.InstallNetwork.DNS
	}
	return profile.DefaultDNS
}
