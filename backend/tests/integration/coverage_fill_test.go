package integration

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"universe/backend/internal/biz"
	"universe/backend/internal/data"
	"universe/backend/tests/integration/testenv"
)

func TestArtifactMultipartUploadOverHTTP(t *testing.T) {
	env := testenv.Start(t)
	base := startFullServer(t, env)
	jar := login(t, base)

	// Build a multipart body: kind + ubuntu_release + file.
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("kind", "kernel")
	_ = w.WriteField("ubuntu_release", "noble")
	fw, _ := w.CreateFormFile("file", "vmlinuz-noble")
	_, _ = fw.Write([]byte("fake-kernel-bytes-for-coverage"))
	_ = w.Close()

	req, _ := http.NewRequest("POST", base+"/api/v1/artifacts", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Cookie", jar.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}

	// The artifact now lists and the sha256 is present.
	code, body, _ := doJSON(t, jar, "GET", base+"/api/v1/artifacts", "")
	if code != 200 || !strings.Contains(body, "vmlinuz-noble") || !strings.Contains(body, "sha256") {
		t.Errorf("artifact not listed: %s", body)
	}
}

func login(t *testing.T, base string) *cookieJar {
	t.Helper()
	jar := &cookieJar{}
	_, _, hdr := doJSON(t, jar, "POST", base+"/api/v1/auth/login",
		`{"username":"admin","password":"change-me-please"}`)
	jar.setFrom(hdr)
	return jar
}

func TestArtifactStoreLifecycleDirect(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	store, err := data.NewArtifactStore(env.Data, t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(ctx,
		&biz.Artifact{Kind: biz.ArtifactKernel, UbuntuRelease: biz.ReleaseNoble, Filename: "k1"},
		strings.NewReader("bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetByID(ctx, saved.ID); err != nil || got.Filename != "k1" {
		t.Errorf("get by id: %v %+v", err, got)
	}
	ref, err := store.ReferencedByRelease(ctx, biz.ReleaseNoble, biz.ArtifactKernel)
	if err != nil {
		t.Fatal(err)
	}
	if ref {
		t.Error("no profile targets noble yet, should not be referenced")
	}
	seedProfile(t, env) // creates a noble profile
	ref, _ = store.ReferencedByRelease(ctx, biz.ReleaseNoble, biz.ArtifactKernel)
	if !ref {
		t.Error("noble kernel should now be referenced by the profile")
	}
	if err := store.Delete(ctx, saved.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.GetByID(ctx, saved.ID); err != biz.ErrEntityNotFound {
		t.Errorf("get after delete: %v", err)
	}
}

func TestDhcpGateDirect(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	gate := data.NewDhcpGate(env.Data)
	enabled, err := gate.Enabled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Error("dhcp gate should report disabled by default")
	}
}
