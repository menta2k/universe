package netboot

import "testing"

func TestIPXEBinariesEmbeddedAndNonEmpty(t *testing.T) {
	bins := IPXEBinaries()
	for _, name := range []string{IPXEBinBIOS, IPXEBinUEFI, IPXEBinARM64} {
		content, ok := bins[name]
		if !ok {
			t.Errorf("missing embedded binary %q", name)
			continue
		}
		if len(content) < 1024 {
			t.Errorf("binary %q suspiciously small: %d bytes", name, len(content))
		}
	}
}
