package bootfiles

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/menta2k/universe/backend/internal/biz"
)

// ArtifactStore is the subset of the artifact store the fetcher needs: check
// whether a release/kind already exists, and store a fetched file.
type ArtifactStore interface {
	GetByReleaseKind(ctx context.Context, release biz.UbuntuRelease, kind biz.ArtifactKind) (*biz.Artifact, error)
	GetByFilename(ctx context.Context, filename string) (*biz.Artifact, error)
	Save(ctx context.Context, meta *biz.Artifact, content io.Reader) (*biz.Artifact, error)
}

// releaseVersion maps the release codename to the Ubuntu version used in the
// default download path.
var releaseVersion = map[biz.UbuntuRelease]string{
	biz.ReleaseNoble: "24.04",
	biz.ReleaseJammy: "22.04",
}

// Config controls the auto-fetch behaviour.
type Config struct {
	// Releases to ensure at startup (defaults to noble + jammy when empty).
	Releases []biz.UbuntuRelease
	// ISOURLs optionally overrides the ISO location per release (mirrors /
	// air-gapped). When absent, the latest live-server ISO is discovered from
	// releases.ubuntu.com.
	ISOURLs map[biz.UbuntuRelease]string
	// ServeISO additionally downloads the full ISO (once, cached) so the
	// daemon can serve the installer's root filesystem over HTTP.
	ServeISO bool
}

// Fetcher ensures kernel/initrd artifacts exist for a release, fetching them
// from the live-server ISO on demand.
type Fetcher struct {
	artifacts ArtifactStore
	client *http.Client
	cfg    Config
	log    *slog.Logger
}

func New(store ArtifactStore, cfg Config, log *slog.Logger) *Fetcher {
	if len(cfg.Releases) == 0 {
		cfg.Releases = []biz.UbuntuRelease{biz.ReleaseNoble, biz.ReleaseJammy}
	}
	return &Fetcher{
		artifacts: store,
		client: &http.Client{Timeout: 30 * time.Minute},
		cfg:    cfg,
		log:    log,
	}
}

// EnsureConfigured ensures boot files for every configured release. Each
// release is best-effort: a failure is logged and does not abort the others.
func (f *Fetcher) EnsureConfigured(ctx context.Context) {
	for _, rel := range f.cfg.Releases {
		if err := f.EnsureRelease(ctx, rel); err != nil {
			f.log.Error("boot-file auto-fetch failed", "release", rel, "err", err)
		}
	}
}

// EnsureRelease fetches kernel and initrd for release if either is missing
// (and the full ISO when ServeISO is set). Present artifacts are left
// untouched, so this is cheap to call repeatedly.
func (f *Fetcher) EnsureRelease(ctx context.Context, release biz.UbuntuRelease) error {
	if _, ok := releaseVersion[release]; !ok {
		return fmt.Errorf("unsupported release %q", release)
	}
	haveKernel := f.exists(ctx, release, biz.ArtifactKernel)
	haveInitrd := f.exists(ctx, release, biz.ArtifactInitrd)
	haveISO := !f.cfg.ServeISO || f.isoExists(ctx, release)
	if haveKernel && haveInitrd && haveISO {
		return nil
	}

	url, err := f.resolveISOURL(ctx, release)
	if err != nil {
		return err
	}

	if !haveKernel || !haveInitrd {
		f.log.Info("fetching boot files from iso", "release", release, "url", url)
		ra, err := newHTTPReaderAt(ctx, f.client, url)
		if err != nil {
			return err
		}
		if !haveKernel {
			if err := f.store(ctx, release, biz.ArtifactKernel, "vmlinuz", ra); err != nil {
				return fmt.Errorf("kernel: %w", err)
			}
		}
		if !haveInitrd {
			if err := f.store(ctx, release, biz.ArtifactInitrd, "initrd", ra); err != nil {
				return fmt.Errorf("initrd: %w", err)
			}
		}
	}
	if !haveISO {
		if err := f.storeISO(ctx, release, url); err != nil {
			return fmt.Errorf("iso: %w", err)
		}
	}
	f.log.Info("boot files ready", "release", release)
	return nil
}

func (f *Fetcher) isoExists(ctx context.Context, release biz.UbuntuRelease) bool {
	_, err := f.artifacts.GetByFilename(ctx, string(release)+".iso")
	return err == nil
}

// storeISO streams the full ISO to the artifact store so it can be served as
// the installer's root filesystem over HTTP.
func (f *Fetcher) storeISO(ctx context.Context, release biz.UbuntuRelease, url string) error {
	f.log.Info("downloading full iso for serving", "release", release, "url", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("get iso: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get iso: status %d", resp.StatusCode)
	}
	_, err = f.artifacts.Save(ctx, &biz.Artifact{
		Kind: biz.ArtifactOther, UbuntuRelease: release, Filename: string(release) + ".iso",
	}, resp.Body)
	return err
}

func (f *Fetcher) exists(ctx context.Context, release biz.UbuntuRelease, kind biz.ArtifactKind) bool {
	_, err := f.artifacts.GetByReleaseKind(ctx, release, kind)
	return err == nil
}

func (f *Fetcher) store(ctx context.Context, release biz.UbuntuRelease, kind biz.ArtifactKind, casperName string, ra *httpReaderAt) error {
	rd, err := extractCasperFile(ra, casperName)
	if err != nil {
		return err
	}
	filename := fmt.Sprintf("%s-%s", release, kind)
	_, err = f.artifacts.Save(ctx, &biz.Artifact{
		Kind: kind, UbuntuRelease: release, Filename: filename,
	}, rd)
	return err
}

// isoHref matches the live-server ISO filename in a releases.ubuntu.com listing.
var isoHref = regexp.MustCompile(`ubuntu-[0-9.]+-live-server-amd64\.iso`)

// resolveISOURL returns the configured override or discovers the latest
// live-server ISO for the release from releases.ubuntu.com.
func (f *Fetcher) resolveISOURL(ctx context.Context, release biz.UbuntuRelease) (string, error) {
	if u, ok := f.cfg.ISOURLs[release]; ok && u != "" {
		return u, nil
	}
	base := fmt.Sprintf("https://releases.ubuntu.com/%s/", releaseVersion[release])
	return f.resolveViaListing(ctx, base)
}

// resolveViaListing fetches an Apache-style directory listing and returns the
// URL of the newest live-server amd64 ISO in it.
func (f *Fetcher) resolveViaListing(ctx context.Context, base string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return "", err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("list %s: %w", base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	names := isoHref.FindAllString(string(body), -1)
	if len(names) == 0 {
		return "", fmt.Errorf("no live-server iso found at %s", base)
	}
	sort.Strings(names)
	return base + names[len(names)-1], nil
}
