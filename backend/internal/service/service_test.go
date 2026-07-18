package service

import (
	"errors"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"

	v1 "universe/backend/api/netboot/v1"
	"universe/backend/internal/biz"
)

// TestMapErr asserts each biz/domain error maps to the expected kratos
// reason + HTTP status code via errors.FromError.
func TestMapErr(t *testing.T) {
	cases := []struct {
		name   string
		in     error
		code   int32
		reason string
	}{
		{"validation", &biz.ValidationError{Fields: map[string]string{"mac": "invalid"}}, 422, "VALIDATION_FAILED"},
		{"not found", biz.ErrEntityNotFound, 404, "NOT_FOUND"},
		{"dhcp disabled", biz.ErrDhcpDisabled, 412, "DHCP_DISABLED"},
		{"session conflict", biz.ErrSessionConflict, 409, "CONFLICT"},
		{"no active session", biz.ErrNoActiveSession, 409, "CONFLICT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			se := kerrors.FromError(mapErr(tc.in))
			if se.Code != tc.code {
				t.Errorf("code = %d, want %d", se.Code, tc.code)
			}
			if se.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", se.Reason, tc.reason)
			}
		})
	}

	// A generic (non-domain) error passes through unchanged; kratos treats it
	// as an unknown 500.
	t.Run("generic", func(t *testing.T) {
		generic := errors.New("boom")
		mapped := mapErr(generic)
		if !errors.Is(mapped, generic) {
			t.Errorf("generic error not passed through: %v", mapped)
		}
		if se := kerrors.FromError(mapped); se.Code != 500 {
			t.Errorf("generic code = %d, want 500", se.Code)
		}
	})
}

func TestMapErrNil(t *testing.T) {
	if err := mapErr(nil); err != nil {
		t.Errorf("mapErr(nil) = %v, want nil", err)
	}
}

// TestMapProfileErr asserts the profile-specific in-use guard maps to 409 and
// that everything else defers to mapErr.
func TestMapProfileErr(t *testing.T) {
	cases := []struct {
		name   string
		in     error
		code   int32
		reason string
	}{
		{"profile in use", biz.ErrProfileInUse, 409, "CONFLICT"},
		{"not found falls through", biz.ErrEntityNotFound, 404, "NOT_FOUND"},
		{"validation falls through", &biz.ValidationError{Fields: map[string]string{"name": "bad"}}, 422, "VALIDATION_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			se := kerrors.FromError(mapProfileErr(tc.in))
			if se.Code != tc.code {
				t.Errorf("code = %d, want %d", se.Code, tc.code)
			}
			if se.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", se.Reason, tc.reason)
			}
		})
	}
}

func TestPageParams(t *testing.T) {
	if page, size := pageParams(nil); page != 1 || size != 50 {
		t.Errorf("pageParams(nil) = (%d,%d), want (1,50)", page, size)
	}
	if page, size := pageParams(&v1.PageRequest{Page: 3, PageSize: 25}); page != 3 || size != 25 {
		t.Errorf("pageParams explicit = (%d,%d), want (3,25)", page, size)
	}
}

func TestUnixToTime(t *testing.T) {
	if got := unixToTime(0); !got.IsZero() {
		t.Errorf("unixToTime(0) = %v, want zero time", got)
	}
	if got := unixToTime(-5); !got.IsZero() {
		t.Errorf("unixToTime(-5) = %v, want zero time", got)
	}
	got := unixToTime(1_700_000_000)
	want := time.Unix(1_700_000_000, 0).UTC()
	if !got.Equal(want) {
		t.Errorf("unixToTime = %v, want %v", got, want)
	}
	if got.Location() != time.UTC {
		t.Errorf("unixToTime location = %v, want UTC", got.Location())
	}
}

func TestToMachineReply(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	m := &biz.Machine{
		ID: "m1", MAC: "52:54:00:ab:cd:ef", Name: "target", Firmware: biz.FirmwareUEFI,
		ProfileID: "p1", ReservationIP: "10.0.0.5", State: biz.StateInstalled,
		Notes: "note", CreatedAt: now, UpdatedAt: now.Add(time.Hour), ActiveSessionID: "s1",
	}
	got := toMachineReply(m)
	if got.Id != "m1" || got.Mac != m.MAC || got.Name != "target" {
		t.Errorf("identity fields wrong: %+v", got)
	}
	if got.Firmware != "uefi_x64" || got.ProvisionState != "installed" {
		t.Errorf("enum fields wrong: firmware=%q state=%q", got.Firmware, got.ProvisionState)
	}
	if got.ProfileId != "p1" || got.ReservationIp != "10.0.0.5" || got.Notes != "note" || got.ActiveSessionId != "s1" {
		t.Errorf("assoc fields wrong: %+v", got)
	}
	if !got.CreatedAt.AsTime().Equal(now) || !got.UpdatedAt.AsTime().Equal(now.Add(time.Hour)) {
		t.Errorf("timestamps wrong: created=%v updated=%v", got.CreatedAt.AsTime(), got.UpdatedAt.AsTime())
	}
}

func TestToProfileReply(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	p := &biz.Profile{
		ID: "p1", Name: "noble", Version: 3, UbuntuRelease: biz.ReleaseNoble,
		StorageLayout: biz.StorageLayout{Mode: "lvm"},
		NetworkConfig: map[string]any{"version": float64(2)},
		Packages:      []string{"curl", "vim"}, SSHAuthorizedKeys: []string{"ssh-ed25519 AAA"},
		UserDataTemplate: "tmpl", LateCommands: []string{"echo hi"}, KernelCmdlineExtra: "quiet",
		CreatedAt: now, UpdatedAt: now, AssignedMachines: 4,
	}
	got := toProfileReply(p)
	if got.Id != "p1" || got.Name != "noble" || got.Version != 3 || got.UbuntuRelease != "noble" {
		t.Errorf("scalar fields wrong: %+v", got)
	}
	if got.StorageLayout != `{"mode":"lvm"}` {
		t.Errorf("storage layout = %q", got.StorageLayout)
	}
	if got.NetworkConfig != `{"version":2}` {
		t.Errorf("network config = %q", got.NetworkConfig)
	}
	if len(got.Packages) != 2 || got.Packages[0] != "curl" {
		t.Errorf("packages = %v", got.Packages)
	}
	if got.AssignedMachines != 4 || got.KernelCmdlineExtra != "quiet" {
		t.Errorf("misc fields wrong: %+v", got)
	}
	if !got.CreatedAt.AsTime().Equal(now) {
		t.Errorf("created_at = %v", got.CreatedAt.AsTime())
	}
}

func TestToProfileReplyNilNetworkConfig(t *testing.T) {
	got := toProfileReply(&biz.Profile{ID: "p", UbuntuRelease: biz.ReleaseJammy})
	if got.NetworkConfig != "{}" {
		t.Errorf("nil network config = %q, want {}", got.NetworkConfig)
	}
}

func TestToDhcpConfigReply(t *testing.T) {
	c := &biz.DhcpConfig{
		Enabled: true, Version: 2, LeaseTTLSeconds: 3600,
		Subnets: []biz.DhcpSubnet{{
			ID: "sn1", Network: "10.0.0.0/24", RangeStart: "10.0.0.10",
			RangeEnd: "10.0.0.99", Gateway: "10.0.0.1", DNS: []string{"1.1.1.1", "8.8.8.8"},
		}},
	}
	got := toDhcpConfigReply(c)
	if !got.Enabled || got.Version != 2 || got.LeaseTtlSeconds != 3600 {
		t.Errorf("scalar fields wrong: %+v", got)
	}
	if len(got.Subnets) != 1 {
		t.Fatalf("subnets = %d, want 1", len(got.Subnets))
	}
	sn := got.Subnets[0]
	if sn.Id != "sn1" || sn.Network != "10.0.0.0/24" || sn.RangeStart != "10.0.0.10" ||
		sn.RangeEnd != "10.0.0.99" || sn.Gateway != "10.0.0.1" {
		t.Errorf("subnet fields wrong: %+v", sn)
	}
	if len(sn.Dns) != 2 || sn.Dns[0] != "1.1.1.1" {
		t.Errorf("subnet dns = %v", sn.Dns)
	}
}

func TestToSessionReply(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	end := start.Add(10 * time.Minute)
	v := &biz.SessionView{
		Session: biz.Session{
			ID: "s1", MachineID: "m1", ProfileID: "p1", ProfileVersion: 2,
			State: biz.SessionCompleted, StartedAt: start, EndedAt: end, FailurePhase: "",
		},
		MachineName: "target", MachineMAC: "52:54:00:ab:cd:ef",
	}
	got := toSessionReply(v)
	if got.Id != "s1" || got.MachineId != "m1" || got.MachineName != "target" || got.MachineMac != v.MachineMAC {
		t.Errorf("identity fields wrong: %+v", got)
	}
	if got.ProfileId != "p1" || got.ProfileVersion != 2 || got.State != "completed" {
		t.Errorf("profile/state fields wrong: %+v", got)
	}
	if !got.StartedAt.AsTime().Equal(start) {
		t.Errorf("started_at = %v", got.StartedAt.AsTime())
	}
	if got.EndedAt == nil || !got.EndedAt.AsTime().Equal(end) {
		t.Errorf("ended_at = %v, want %v", got.EndedAt, end)
	}
}

func TestToSessionReplyZeroEndedAt(t *testing.T) {
	v := &biz.SessionView{
		Session: biz.Session{ID: "s1", State: biz.SessionActive, StartedAt: time.Now()},
	}
	got := toSessionReply(v)
	if got.EndedAt != nil {
		t.Errorf("ended_at = %v, want nil for active session", got.EndedAt)
	}
}

func TestParseInput(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		in := &v1.ProfileInput{
			Name: "noble", UbuntuRelease: "noble",
			StorageLayout: `{"mode":"lvm"}`,
			NetworkConfig: `{"version":2}`,
			Packages:      []string{"curl"},
		}
		out, err := parseInput(in)
		if err != nil {
			t.Fatalf("parseInput valid: %v", err)
		}
		if out.Name != "noble" || out.UbuntuRelease != biz.ReleaseNoble {
			t.Errorf("scalar fields wrong: %+v", out)
		}
		if out.StorageLayout.Mode != "lvm" {
			t.Errorf("storage layout mode = %q", out.StorageLayout.Mode)
		}
		if out.NetworkConfig["version"] != float64(2) {
			t.Errorf("network config = %v", out.NetworkConfig)
		}
	})

	t.Run("empty storage and network skipped", func(t *testing.T) {
		out, err := parseInput(&v1.ProfileInput{Name: "n", UbuntuRelease: "noble", NetworkConfig: "{}"})
		if err != nil {
			t.Fatalf("parseInput empty: %v", err)
		}
		if out.NetworkConfig != nil {
			t.Errorf("network config = %v, want nil for {}", out.NetworkConfig)
		}
	})

	t.Run("invalid storage_layout JSON", func(t *testing.T) {
		_, err := parseInput(&v1.ProfileInput{StorageLayout: "{not-json"})
		se := kerrors.FromError(err)
		if se.Code != 422 || se.Reason != "VALIDATION_FAILED" {
			t.Errorf("err = code %d reason %q, want 422 VALIDATION_FAILED", se.Code, se.Reason)
		}
		if se.Metadata["storage_layout"] == "" {
			t.Errorf("expected storage_layout detail, got %v", se.Metadata)
		}
	})

	t.Run("invalid network_config JSON", func(t *testing.T) {
		_, err := parseInput(&v1.ProfileInput{NetworkConfig: "{not-json"})
		se := kerrors.FromError(err)
		if se.Code != 422 || se.Reason != "VALIDATION_FAILED" {
			t.Errorf("err = code %d reason %q, want 422 VALIDATION_FAILED", se.Code, se.Reason)
		}
		if se.Metadata["network_config"] == "" {
			t.Errorf("expected network_config detail, got %v", se.Metadata)
		}
	})
}

func TestToArtifactReply(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	a := &biz.Artifact{
		ID: "a1", Kind: biz.ArtifactKernel, UbuntuRelease: biz.ReleaseNoble,
		Filename: "vmlinuz", SizeBytes: 4096, SHA256: "deadbeef", UploadedBy: "op1",
		CreatedAt: now, UpdatedAt: now,
	}
	got := toArtifactReply(a)
	if got.Id != "a1" || got.Kind != "kernel" || got.UbuntuRelease != "noble" {
		t.Errorf("scalar fields wrong: %+v", got)
	}
	if got.Filename != "vmlinuz" || got.SizeBytes != 4096 || got.Sha256 != "deadbeef" || got.UploadedBy != "op1" {
		t.Errorf("file fields wrong: %+v", got)
	}
	if !got.CreatedAt.AsTime().Equal(now) {
		t.Errorf("created_at = %v", got.CreatedAt.AsTime())
	}
}

func TestMapArtifactErr(t *testing.T) {
	se := kerrors.FromError(mapArtifactErr(biz.ErrArtifactInUse))
	if se.Code != 409 || se.Reason != "CONFLICT" {
		t.Errorf("in-use = code %d reason %q, want 409 CONFLICT", se.Code, se.Reason)
	}
	se = kerrors.FromError(mapArtifactErr(biz.ErrEntityNotFound))
	if se.Code != 404 {
		t.Errorf("not found falls through = %d, want 404", se.Code)
	}
}

func TestToOperatorReply(t *testing.T) {
	op := &biz.Operator{ID: "op1", Username: "admin", DisplayName: "Admin", Active: true}
	got := toOperatorReply(op)
	if got.Id != "op1" || got.Username != "admin" || got.DisplayName != "Admin" || !got.Active {
		t.Errorf("operator reply wrong: %+v", got)
	}
}
