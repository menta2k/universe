package dhcp

import (
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"

	"universe/backend/internal/biz"
)

func TestFirmwareOf(t *testing.T) {
	cases := []struct {
		name  string
		archs []iana.Arch
		want  biz.Firmware
	}{
		{"no option 93 -> bios", nil, biz.FirmwareBIOS},
		{"intel x86 -> bios", []iana.Arch{iana.INTEL_X86PC}, biz.FirmwareBIOS},
		{"efi x64 -> uefi", []iana.Arch{iana.EFI_X86_64}, biz.FirmwareUEFI},
		{"efi bc -> uefi", []iana.Arch{iana.EFI_BC}, biz.FirmwareUEFI},
		{"unsupported arch -> unknown", []iana.Arch{iana.EFI_ARM64}, biz.FirmwareUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirmwareOf(tc.archs); got != tc.want {
				t.Errorf("FirmwareOf(%v) = %s, want %s", tc.archs, got, tc.want)
			}
		})
	}
}

// mkPacket builds a DISCOVER with the given options for decision testing.
func mkPacket(t *testing.T, opts ...dhcpv4.Modifier) *dhcpv4.DHCPv4 {
	t.Helper()
	m, err := dhcpv4.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func withArch(a iana.Arch) dhcpv4.Modifier {
	return dhcpv4.WithOption(dhcpv4.OptClientArch(a))
}

func withClass(s string) dhcpv4.Modifier {
	return dhcpv4.WithOption(dhcpv4.OptClassIdentifier(s))
}

func withUserClass(s string) dhcpv4.Modifier {
	return dhcpv4.WithOption(dhcpv4.OptUserClass(s))
}

func TestStageDetection(t *testing.T) {
	t.Run("plain client is not netboot", func(t *testing.T) {
		if got := stageOf(mkPacket(t)); got != StageNotNetboot {
			t.Errorf("stage = %d, want StageNotNetboot", got)
		}
	})
	t.Run("pxe firmware first request", func(t *testing.T) {
		m := mkPacket(t, withClass("PXEClient"), withArch(iana.EFI_X86_64))
		if got := stageOf(m); got != StageFirmware {
			t.Errorf("stage = %d, want StageFirmware", got)
		}
	})
	t.Run("ipxe user-class breaks the loop", func(t *testing.T) {
		m := mkPacket(t, withClass("PXEClient"), withArch(iana.EFI_X86_64), withUserClass("iPXE"))
		if got := stageOf(m); got != StageIPXE {
			t.Errorf("stage = %d, want StageIPXE", got)
		}
	})
}

func TestDecideBoot(t *testing.T) {
	const serverIP = "192.0.2.1"
	const bootURL = "http://192.0.2.1:8082"
	const mac = "52:54:00:aa:bb:cc"

	t.Run("bios firmware gets undionly over tftp", func(t *testing.T) {
		m := mkPacket(t, withClass("PXEClient"), withArch(iana.INTEL_X86PC))
		br := decideBoot(m, serverIP, bootURL, mac)
		if br.Filename != bootfileBIOS || br.NextServer != serverIP {
			t.Errorf("bios boot = %+v", br)
		}
	})
	t.Run("uefi firmware gets ipxe.efi over tftp", func(t *testing.T) {
		m := mkPacket(t, withClass("PXEClient"), withArch(iana.EFI_X86_64))
		br := decideBoot(m, serverIP, bootURL, mac)
		if br.Filename != bootfileUEFI || br.NextServer != serverIP {
			t.Errorf("uefi boot = %+v", br)
		}
	})
	t.Run("ipxe stage gets http script url, no next-server", func(t *testing.T) {
		m := mkPacket(t, withClass("PXEClient"), withArch(iana.EFI_X86_64), withUserClass("iPXE"))
		br := decideBoot(m, serverIP, bootURL, mac)
		want := bootURL + "/boot/ipxe/" + mac
		if br.Filename != want {
			t.Errorf("ipxe filename = %q, want %q", br.Filename, want)
		}
		if br.NextServer != "" {
			t.Errorf("ipxe stage should not set next-server, got %q", br.NextServer)
		}
	})
	t.Run("non-netboot client gets no boot options", func(t *testing.T) {
		br := decideBoot(mkPacket(t), serverIP, bootURL, mac)
		if br.Filename != "" {
			t.Errorf("expected no filename, got %q", br.Filename)
		}
	})
}
