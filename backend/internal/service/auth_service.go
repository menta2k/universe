// Package service maps proto RPCs to biz use cases.
package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"google.golang.org/protobuf/types/known/emptypb"

	v1 "github.com/menta2k/universe/backend/api/netboot/v1"
	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/server"
)

// SessionCookie is the browser session cookie name.
const SessionCookie = "nb_session"

type AuthService struct {
	v1.UnimplementedAuthServiceServer
	operators *biz.OperatorUsecase
}

func NewAuthService(operators *biz.OperatorUsecase) *AuthService {
	return &AuthService{operators: operators}
}

func toOperatorReply(op *biz.Operator) *v1.Operator {
	return &v1.Operator{
		Id:          op.ID,
		Username:    op.Username,
		DisplayName: op.DisplayName,
		Active:      op.Active,
	}
}

func (s *AuthService) Login(ctx context.Context, req *v1.LoginRequest) (*v1.Operator, error) {
	if req.GetUsername() == "" || req.GetPassword() == "" {
		return nil, server.ErrValidation("username and password are required", nil)
	}
	op, token, err := s.operators.Login(ctx, req.GetUsername(), req.GetPassword())
	if err != nil {
		return nil, server.ErrUnauthenticated()
	}
	setSessionCookie(ctx, token, false)
	return toOperatorReply(op), nil
}

func (s *AuthService) Logout(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if token := sessionTokenFromContext(ctx); token != "" {
		if err := s.operators.Logout(ctx, token); err != nil {
			return nil, err
		}
	}
	setSessionCookie(ctx, "", true)
	return &emptypb.Empty{}, nil
}

func (s *AuthService) Me(ctx context.Context, _ *emptypb.Empty) (*v1.Operator, error) {
	op, ok := server.OperatorFromContext(ctx)
	if !ok {
		return nil, server.ErrUnauthenticated()
	}
	return toOperatorReply(op), nil
}

// sessionTokenFromContext extracts the session cookie from the HTTP request.
func sessionTokenFromContext(ctx context.Context) string {
	tr, ok := transport.FromServerContext(ctx)
	if !ok {
		return ""
	}
	ht, ok := tr.(khttp.Transporter)
	if !ok {
		return ""
	}
	c, err := ht.Request().Cookie(SessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// setSessionCookie writes/clears the session cookie on the HTTP response.
func setSessionCookie(ctx context.Context, token string, expire bool) {
	tr, ok := transport.FromServerContext(ctx)
	if !ok {
		return
	}
	header := tr.ReplyHeader()
	cookie := SessionCookie + "=" + token + "; Path=/; HttpOnly; SameSite=Strict"
	if expire {
		cookie += "; Max-Age=0"
	}
	header.Set("Set-Cookie", cookie)
}
