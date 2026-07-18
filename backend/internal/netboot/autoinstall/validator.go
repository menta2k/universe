package autoinstall

import (
	"universe/backend/internal/biz"
)

// Validator implements biz.AutoinstallValidator by rendering a profile against
// a synthetic fixture machine and checking the produced document (FR-008).
type Validator struct{}

func NewValidator() *Validator { return &Validator{} }

// fixtureInput builds a representative render input for a profile so that
// validation exercises the same path a real boot would.
func fixtureInput(p *biz.Profile) Input {
	return Input{
		Machine: &biz.Machine{ID: "fixture", MAC: "52:54:00:00:00:01", Name: "fixture-host"},
		Profile: p,
		Session: &biz.Session{ID: "fixture-session"},
		BootURL: "http://validation.invalid:8082",
		// A syntactically valid argon2id hash so identity validation passes.
		SeedToken:           "validationtoken",
		OneTimePasswordHash: "$argon2id$v=19$m=65536,t=1,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}
}

// Validate renders and structurally checks the profile.
func (Validator) Validate(p *biz.Profile) error {
	in := fixtureInput(p)
	if _, _, err := Render(in); err != nil {
		return err
	}
	if _, err := Cmdline(in); err != nil {
		return err
	}
	return nil
}

// PreviewRedacted renders the user-data with credentials replaced by a
// placeholder, for the profile preview endpoint.
func PreviewRedacted(m *biz.Machine, p *biz.Profile) (userData, cmdline string, err error) {
	in := Input{
		Machine: m, Profile: p, Session: &biz.Session{ID: "preview"},
		BootURL: "http://preview.invalid:8082", SeedToken: "PREVIEW",
		OneTimePasswordHash: "<redacted-one-time-hash>",
	}
	if m == nil {
		in.Machine = &biz.Machine{ID: "preview", MAC: "52:54:00:00:00:01", Name: "preview-host"}
	}
	ud, _, err := Render(in)
	if err != nil {
		return "", "", err
	}
	cl, err := Cmdline(in)
	if err != nil {
		return "", "", err
	}
	return ud, cl, nil
}
