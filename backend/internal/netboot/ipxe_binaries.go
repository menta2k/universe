// Package netboot hosts the machine-facing protocol servers (DHCP, TFTP,
// boot HTTP) and shared boot assets.
package netboot

import (
	"github.com/tinkerbell/ipxedust/binary"
)

// iPXE binary names served over TFTP/HTTP; keys are the exact filenames
// referenced in DHCP boot options (contracts/boot-protocols.md).
const (
	IPXEBinBIOS    = "undionly.kpxe"
	IPXEBinUEFI    = "ipxe.efi"
	IPXEBinARM64   = "snp.efi"
	IPXEUserClass  = "iPXE" // option 77 value emitted by iPXE builds
)

// IPXEBinaries maps served filename -> embedded binary content.
func IPXEBinaries() map[string][]byte {
	return map[string][]byte{
		IPXEBinBIOS:  binary.Undionly,
		IPXEBinUEFI:  binary.IpxeEFI,
		IPXEBinARM64: binary.SNP,
	}
}
