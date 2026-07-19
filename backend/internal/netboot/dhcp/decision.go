// Package dhcp implements the authoritative DHCPv4 service: address leasing
// plus per-machine netboot decisions (arch detection, iPXE chainload).
package dhcp

import (
	"slices"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"

	"github.com/menta2k/universe/backend/internal/biz"
)

// Boot filenames served per firmware type (contracts/boot-protocols.md §1).
const (
	bootfileBIOS = "undionly.kpxe"
	bootfileUEFI = "ipxe.efi"
	bootfileARM  = "snp.efi"
	// iPXEUserClass is option 77's value once our iPXE binary is running.
	iPXEUserClass = "iPXE"
)

// BootStage classifies where a client is in the chainload sequence.
type BootStage int

const (
	// StageNotNetboot: ordinary DHCP client, serve a plain lease only.
	StageNotNetboot BootStage = iota
	// StageFirmware: PXE firmware's first request — serve the iPXE binary.
	StageFirmware
	// StageIPXE: our iPXE is running — redirect to the HTTP boot script.
	StageIPXE
)

// FirmwareOf maps a client's DHCP arch option (93) to our firmware enum.
func FirmwareOf(archs []iana.Arch) biz.Firmware {
	for _, a := range archs {
		switch a {
		case iana.INTEL_X86PC:
			return biz.FirmwareBIOS
		case iana.EFI_X86_64, iana.EFI_BC, iana.EFI_X86_64_HTTP:
			return biz.FirmwareUEFI
		}
	}
	if len(archs) > 0 {
		// An arch was announced but it's not one we serve x86 binaries for.
		return biz.FirmwareUnknown
	}
	return biz.FirmwareBIOS // no option 93 → legacy BIOS PXE
}

// bootfileFor returns the TFTP filename for a firmware type.
func bootfileFor(fw biz.Firmware) string {
	switch fw {
	case biz.FirmwareUEFI:
		return bootfileUEFI
	default:
		return bootfileBIOS
	}
}

// isNetbootRequest reports whether the packet is a PXE/network-boot request
// (option 60 PXEClient/HTTPClient AND an architecture option present).
func isNetbootRequest(m *dhcpv4.DHCPv4) bool {
	class := m.ClassIdentifier()
	hasClass := len(class) >= 9 && (class[:9] == "PXEClient" || class[:9] == "HTTPClien")
	return hasClass && len(m.ClientArch()) > 0
}

// isIPXE reports whether option 77 marks the client as our running iPXE.
func isIPXE(m *dhcpv4.DHCPv4) bool {
	return slices.Contains(m.UserClass(), iPXEUserClass)
}

// stageOf determines the boot stage from the request.
func stageOf(m *dhcpv4.DHCPv4) BootStage {
	if !isNetbootRequest(m) {
		return StageNotNetboot
	}
	if isIPXE(m) {
		return StageIPXE
	}
	return StageFirmware
}

// BootResponse describes what to put in the DHCP reply's boot fields for an
// armed machine. Empty Filename means "no netboot options".
type BootResponse struct {
	Filename   string // option 67 / BootFileName
	NextServer string // option 66 / server IP for TFTP or HTTP
	Firmware   biz.Firmware
}

// decideBoot computes boot options for a request from an armed machine.
//   - StageFirmware  → serve the arch-appropriate iPXE binary over TFTP.
//   - StageIPXE      → hand our iPXE the HTTP boot-script URL.
//   - StageNotNetboot→ no boot options (plain lease).
func decideBoot(m *dhcpv4.DHCPv4, serverIP, bootHTTPURL, mac string) BootResponse {
	fw := FirmwareOf(m.ClientArch())
	switch stageOf(m) {
	case StageFirmware:
		return BootResponse{Filename: bootfileFor(fw), NextServer: serverIP, Firmware: fw}
	case StageIPXE:
		return BootResponse{Filename: bootHTTPURL + "/boot/ipxe/" + mac, Firmware: fw}
	default:
		return BootResponse{Firmware: fw}
	}
}
