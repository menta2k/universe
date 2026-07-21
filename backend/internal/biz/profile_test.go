package biz

import (
	"context"
	"errors"
	"testing"
)

// stubValidator lets tests force render success/failure.
type stubValidator struct{ err error }

func (s stubValidator) Validate(*Profile) error { return s.err }

// stubHasher returns a deterministic "hash" so tests can assert a password was
// hashed and stored without depending on the real crypt implementation.
type stubHasher struct{}

func (stubHasher) Hash(plaintext string) (string, error) { return "$6$test$" + plaintext, nil }

func newProfileUC(t *testing.T, validatorErr error) (*ProfileUsecase, *fakeProfileRepo) {
	t.Helper()
	repo := &fakeProfileRepo{byID: map[string]*Profile{}}
	return NewProfileUsecase(repo, stubValidator{err: validatorErr}, stubHasher{}, testLogger()), repo
}

func validInput() ProfileInput {
	return ProfileInput{
		Name: "noble-web", UbuntuRelease: ReleaseNoble,
		StorageLayout:     StorageLayout{Mode: "lvm"},
		SSHAuthorizedKeys: []string{"ssh-ed25519 AAAA test"},
	}
}

func TestProfileCreateValidation(t *testing.T) {
	uc, _ := newProfileUC(t, nil)
	cases := []struct {
		name  string
		mut   func(*ProfileInput)
		field string
	}{
		{"no name", func(in *ProfileInput) { in.Name = "" }, "name"},
		{"bad release", func(in *ProfileInput) { in.UbuntuRelease = "focal" }, "ubuntu_release"},
		{"bad storage", func(in *ProfileInput) { in.StorageLayout.Mode = "raid" }, "storage_layout"},
		{"custom without body", func(in *ProfileInput) { in.StorageLayout = StorageLayout{Mode: "custom"} }, "storage_layout"},
		{"no ssh keys", func(in *ProfileInput) { in.SSHAuthorizedKeys = nil }, "ssh_authorized_keys"},
		{"cmdline newline", func(in *ProfileInput) { in.KernelCmdlineExtra = "a\nb" }, "kernel_cmdline_extra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.mut(&in)
			_, err := uc.Create(context.Background(), in)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected ValidationError, got %v", err)
			}
			if ve.Fields[tc.field] == "" {
				t.Errorf("expected field %q, got %v", tc.field, ve.Fields)
			}
		})
	}
}

func TestProfileCreateRejectsInvalidRender(t *testing.T) {
	uc, _ := newProfileUC(t, errors.New("bad autoinstall"))
	_, err := uc.Create(context.Background(), validInput())
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("render failure should surface as ValidationError, got %v", err)
	}
}

func TestProfileUpdateBumpsVersion(t *testing.T) {
	uc, repo := newProfileUC(t, nil)
	created, err := uc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 {
		t.Errorf("initial version = %d, want 1", created.Version)
	}
	in := validInput()
	in.Packages = []string{"nginx"}
	updated, err := uc.Update(context.Background(), created.ID, in)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("version after update = %d, want 2", updated.Version)
	}
	if repo.byID[created.ID].Version != 2 {
		t.Error("repo not updated")
	}
}

func TestProfileInstallIdentity(t *testing.T) {
	t.Run("password-only profile is valid and hashes the password", func(t *testing.T) {
		uc, _ := newProfileUC(t, nil)
		in := validInput()
		in.SSHAuthorizedKeys = nil // no keys...
		in.Password = "s3cret"     // ...but a password
		in.InstallUsername = "operator"
		p, err := uc.Create(context.Background(), in)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if p.InstallUsername != "operator" {
			t.Errorf("username = %q, want operator", p.InstallUsername)
		}
		if p.InstallPasswordHash != "$6$test$s3cret" {
			t.Errorf("password hash = %q, want hashed value (never plaintext)", p.InstallPasswordHash)
		}
	})

	t.Run("neither keys nor password is rejected", func(t *testing.T) {
		uc, _ := newProfileUC(t, nil)
		in := validInput()
		in.SSHAuthorizedKeys = nil
		_, err := uc.Create(context.Background(), in)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Fields["ssh_authorized_keys"] == "" {
			t.Fatalf("want access validation error, got %v", err)
		}
	})

	t.Run("bad username is rejected", func(t *testing.T) {
		uc, _ := newProfileUC(t, nil)
		in := validInput()
		in.InstallUsername = "Bad Name"
		_, err := uc.Create(context.Background(), in)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Fields["install_username"] == "" {
			t.Fatalf("want install_username error, got %v", err)
		}
	})

	t.Run("update keeps password when omitted and clears on request", func(t *testing.T) {
		uc, _ := newProfileUC(t, nil)
		in := validInput()
		in.Password = "keepme"
		created, err := uc.Create(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		// Update without a password preserves the stored hash.
		kept, err := uc.Update(context.Background(), created.ID, validInput())
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if kept.InstallPasswordHash != "$6$test$keepme" {
			t.Errorf("password not preserved: %q", kept.InstallPasswordHash)
		}
		// ClearPassword removes it (still valid because the profile has keys).
		in2 := validInput()
		in2.ClearPassword = true
		cleared, err := uc.Update(context.Background(), created.ID, in2)
		if err != nil {
			t.Fatalf("update clear: %v", err)
		}
		if cleared.InstallPasswordHash != "" {
			t.Errorf("password not cleared: %q", cleared.InstallPasswordHash)
		}
	})

	t.Run("clearing the only access method is rejected", func(t *testing.T) {
		uc, _ := newProfileUC(t, nil)
		in := validInput()
		in.SSHAuthorizedKeys = nil
		in.Password = "only-access"
		created, err := uc.Create(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		in2 := validInput()
		in2.SSHAuthorizedKeys = nil
		in2.ClearPassword = true
		_, err = uc.Update(context.Background(), created.ID, in2)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("want validation error clearing sole access method, got %v", err)
		}
	})
}

func TestProfileClone(t *testing.T) {
	uc, _ := newProfileUC(t, nil)
	created, err := uc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatal(err)
	}
	clone, err := uc.Clone(context.Background(), created.ID, "noble-web-copy")
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if clone.ID == created.ID || clone.Name != "noble-web-copy" || clone.Version != 1 {
		t.Errorf("unexpected clone: %+v", clone)
	}
}

func TestProfileDeleteInUse(t *testing.T) {
	repo := &fakeProfileRepo{byID: map[string]*Profile{}}
	repo.deleteErr = ErrProfileInUse
	uc := NewProfileUsecase(repo, stubValidator{}, stubHasher{}, testLogger())
	if err := uc.Delete(context.Background(), "p1"); !errors.Is(err, ErrProfileInUse) {
		t.Errorf("expected ErrProfileInUse, got %v", err)
	}
}
