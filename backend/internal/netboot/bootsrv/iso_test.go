package bootsrv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/menta2k/universe/backend/internal/biz"
)

// fakeArtifacts implements ArtifactSource for the ISO serving tests.
type fakeArtifacts struct {
	byName map[string]*biz.Artifact
}

func (f fakeArtifacts) GetByReleaseKind(context.Context, biz.UbuntuRelease, biz.ArtifactKind) (*biz.Artifact, error) {
	return nil, biz.ErrEntityNotFound
}

func (f fakeArtifacts) GetByFilename(_ context.Context, name string) (*biz.Artifact, error) {
	if a, ok := f.byName[name]; ok {
		return a, nil
	}
	return nil, biz.ErrEntityNotFound
}

func (f fakeArtifacts) Open(_ context.Context, a *biz.Artifact) (io.ReadCloser, error) {
	return os.Open(a.Path)
}

func TestIPXEScriptInjectsISOURLWhenServing(t *testing.T) {
	dec := &biz.BootDecision{Profile: &biz.Profile{UbuntuRelease: biz.ReleaseNoble}}

	on := &Server{externalURL: "http://boot.example:8082", opts: BootOptions{ServeISO: true}}
	script := on.ipxeScript(dec, "autoinstall ds=nocloud;s=http://x/")
	// casper needs the .iso-suffixed URL, ip=dhcp, and the ramdisk root.
	if !strings.Contains(script, "url=http://boot.example:8082/boot/iso/noble.iso") {
		t.Errorf("expected .iso url in kernel cmdline, got:\n%s", script)
	}
	if !strings.Contains(script, "ip=dhcp") {
		t.Errorf("expected ip=dhcp in cmdline, got:\n%s", script)
	}
	if !strings.Contains(script, "root=/dev/ram0 ramdisk_size=1500000") {
		t.Errorf("expected ramdisk root in cmdline, got:\n%s", script)
	}
	// The autoinstall datasource must still be present.
	if !strings.Contains(script, "autoinstall ds=nocloud;s=http://x/") {
		t.Errorf("autoinstall seed lost, got:\n%s", script)
	}

	off := &Server{externalURL: "http://boot.example:8082", opts: BootOptions{ServeISO: false}}
	if s := off.ipxeScript(dec, "autoinstall"); strings.Contains(s, "url=") {
		t.Errorf("iso url must not appear when serveISO is off, got:\n%s", s)
	}
}

func TestIPXEScriptNFSRootCmdline(t *testing.T) {
	dec := &biz.BootDecision{Profile: &biz.Profile{UbuntuRelease: biz.ReleaseNoble}}
	s := &Server{externalURL: "http://boot.example:8082",
		opts: BootOptions{NFSRoot: true, ServeISO: true, NFSServerIP: "10.1.114.3"}}
	script := s.ipxeScript(dec, "autoinstall ds=nocloud;s=http://x/")

	// NFS takes precedence over ServeISO: no url=/ram0, yes netboot=nfs.
	if strings.Contains(script, "url=") || strings.Contains(script, "root=/dev/ram0") {
		t.Errorf("NFS mode must not use the url=/ramdisk path, got:\n%s", script)
	}
	for _, want := range []string{
		"netboot=nfs", "boot=casper", "nfsroot=10.1.114.3:/noble",
		"ip=dhcp", "autoinstall ds=nocloud;s=http://x/",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("NFS cmdline missing %q, got:\n%s", want, script)
		}
	}
	// Networking must stay under cloud-init control so the installer receives a
	// resolver from DHCP; network-config=disabled left resolv.conf empty and apt
	// failed (exit 100) on every extra-package install.
	if strings.Contains(script, "network-config=disabled") {
		t.Errorf("NFS cmdline must not disable cloud-init networking, got:\n%s", script)
	}
}

func TestHandleISOServesFileWithRanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noble.iso")
	content := []byte("PRETEND-ISO-BYTES-0123456789")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		artifacts: fakeArtifacts{byName: map[string]*biz.Artifact{
			"noble.iso": {Filename: "noble.iso", Path: path},
		}},
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Full fetch.
	resp, err := http.Get(ts.URL + "/boot/iso/noble.iso")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != string(content) {
		t.Errorf("full body = %q, want %q", body, content)
	}

	// Range fetch (casper uses these).
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/boot/iso/noble.iso", nil)
	req.Header.Set("Range", "bytes=0-3")
	rr, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rb, _ := io.ReadAll(rr.Body)
	_ = rr.Body.Close()
	if rr.StatusCode != http.StatusPartialContent {
		t.Errorf("range status = %d, want 206", rr.StatusCode)
	}
	if string(rb) != "PRET" {
		t.Errorf("range body = %q, want PRET", rb)
	}
}

func TestHandleISOMissingIs404(t *testing.T) {
	srv := &Server{artifacts: fakeArtifacts{byName: map[string]*biz.Artifact{}}}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/boot/iso/noble.iso")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
