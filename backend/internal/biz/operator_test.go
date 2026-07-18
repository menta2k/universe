package biz

import (
	"context"
	"strings"
	"testing"
)

type fakeOperatorRepo struct {
	byUsername map[string]*Operator
	created    []*Operator
	loginStamp int
}

func newFakeOperatorRepo() *fakeOperatorRepo {
	return &fakeOperatorRepo{byUsername: map[string]*Operator{}}
}

func (f *fakeOperatorRepo) GetByUsername(_ context.Context, u string) (*Operator, error) {
	op, ok := f.byUsername[u]
	if !ok {
		return nil, ErrEntityNotFound
	}
	return op, nil
}

func (f *fakeOperatorRepo) GetByID(_ context.Context, id string) (*Operator, error) {
	for _, op := range f.byUsername {
		if op.ID == id {
			return op, nil
		}
	}
	return nil, ErrEntityNotFound
}

func (f *fakeOperatorRepo) Create(_ context.Context, op *Operator) (*Operator, error) {
	stored := *op
	stored.ID = "op-" + op.Username
	f.byUsername[op.Username] = &stored
	f.created = append(f.created, &stored)
	return &stored, nil
}

func (f *fakeOperatorRepo) Count(_ context.Context) (int, error) {
	return len(f.byUsername), nil
}

func (f *fakeOperatorRepo) TouchLogin(_ context.Context, _ string) error {
	f.loginStamp++
	return nil
}

type fakeSessions struct {
	sessions map[string]string
}

func newFakeSessions() *fakeSessions { return &fakeSessions{sessions: map[string]string{}} }

func (f *fakeSessions) Create(_ context.Context, operatorID string) (string, error) {
	tok := "tok-" + operatorID
	f.sessions[tok] = operatorID
	return tok, nil
}

func (f *fakeSessions) Get(_ context.Context, token string) (string, error) {
	id, ok := f.sessions[token]
	if !ok {
		return "", ErrEntityNotFound
	}
	return id, nil
}

func (f *fakeSessions) Delete(_ context.Context, token string) error {
	delete(f.sessions, token)
	return nil
}

func newOperatorUC(t *testing.T) (*OperatorUsecase, *fakeOperatorRepo, *fakeSessions) {
	t.Helper()
	repo := newFakeOperatorRepo()
	sess := newFakeSessions()
	return NewOperatorUsecase(repo, sess, testLogger()), repo, sess
}

func TestEnsureBootstrapCreatesFirstOperator(t *testing.T) {
	uc, repo, _ := newOperatorUC(t)
	if err := uc.EnsureBootstrap(context.Background(), "admin", "super-secret-pw"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created %d operators, want 1", len(repo.created))
	}
	if repo.created[0].PasswordHash == "super-secret-pw" || repo.created[0].PasswordHash == "" {
		t.Error("password must be stored hashed")
	}
	if !strings.HasPrefix(repo.created[0].PasswordHash, "$argon2id$") {
		t.Errorf("hash is not argon2id: %s", repo.created[0].PasswordHash[:12])
	}
	// Second call must be a no-op (operators exist).
	if err := uc.EnsureBootstrap(context.Background(), "admin2", "super-secret-pw"); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if len(repo.created) != 1 {
		t.Error("bootstrap must not create when operators exist")
	}
}

func TestLoginSuccessAndFailure(t *testing.T) {
	uc, repo, sess := newOperatorUC(t)
	if err := uc.EnsureBootstrap(context.Background(), "admin", "correct-horse-batt"); err != nil {
		t.Fatal(err)
	}

	op, token, err := uc.Login(context.Background(), "admin", "correct-horse-batt")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if op.Username != "admin" || token == "" {
		t.Errorf("unexpected login result: %+v token=%q", op, token)
	}
	if sess.sessions[token] != op.ID {
		t.Error("session not created")
	}
	if repo.loginStamp != 1 {
		t.Error("last_login_at not touched")
	}

	if _, _, err := uc.Login(context.Background(), "admin", "wrong"); err == nil {
		t.Error("wrong password must fail")
	}
	if _, _, err := uc.Login(context.Background(), "ghost", "whatever"); err == nil {
		t.Error("unknown user must fail")
	}
}

func TestLoginInactiveOperator(t *testing.T) {
	uc, repo, _ := newOperatorUC(t)
	if err := uc.EnsureBootstrap(context.Background(), "admin", "correct-horse-batt"); err != nil {
		t.Fatal(err)
	}
	repo.byUsername["admin"].Active = false
	if _, _, err := uc.Login(context.Background(), "admin", "correct-horse-batt"); err == nil {
		t.Error("inactive operator must not log in")
	}
}

func TestAuthenticateSession(t *testing.T) {
	uc, _, _ := newOperatorUC(t)
	if err := uc.EnsureBootstrap(context.Background(), "admin", "correct-horse-batt"); err != nil {
		t.Fatal(err)
	}
	_, token, err := uc.Login(context.Background(), "admin", "correct-horse-batt")
	if err != nil {
		t.Fatal(err)
	}
	op, err := uc.AuthenticateSession(context.Background(), token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if op.Username != "admin" {
		t.Errorf("wrong operator: %+v", op)
	}
	if _, err := uc.AuthenticateSession(context.Background(), "bogus"); err == nil {
		t.Error("bogus token must fail")
	}

	if err := uc.Logout(context.Background(), token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := uc.AuthenticateSession(context.Background(), token); err == nil {
		t.Error("session must be gone after logout")
	}
}
