package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"universe/backend/internal/biz"
)

type operatorCtxKey struct{}

// OperatorFromContext returns the authenticated operator, if any.
func OperatorFromContext(ctx context.Context) (*biz.Operator, bool) {
	op, ok := ctx.Value(operatorCtxKey{}).(*biz.Operator)
	return op, ok
}

// publicOperations need no session (everything else does).
var publicOperations = map[string]bool{
	"/netboot.v1.AuthService/Login": true,
}

// AuthMiddleware authenticates the session cookie and stashes the operator
// in the context. FR-013: all admin API access requires a logged-in operator.
func AuthMiddleware(operators *biz.OperatorUsecase, cookieName string) middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			tr, ok := transport.FromServerContext(ctx)
			if !ok {
				return nil, ErrUnauthenticated()
			}
			if publicOperations[tr.Operation()] {
				return next(ctx, req)
			}
			token := cookieToken(tr, cookieName)
			op, err := operators.AuthenticateSession(ctx, token)
			if err != nil {
				return nil, ErrUnauthenticated()
			}
			return next(context.WithValue(ctx, operatorCtxKey{}, op), req)
		}
	}
}

func cookieToken(tr transport.Transporter, cookieName string) string {
	ht, ok := tr.(khttp.Transporter)
	if !ok {
		return ""
	}
	c, err := ht.Request().Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// AuditMiddleware records a config_change event for every state-changing API
// call, attributed to the operator (FR-013). Reads (GET) are not audited.
func AuditMiddleware(events *biz.EventRecorder) middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			reply, err := next(ctx, req)

			tr, trOK := transport.FromServerContext(ctx)
			if !trOK || isReadOnly(tr) {
				return reply, err
			}
			op, opOK := OperatorFromContext(ctx)
			if !opOK {
				return reply, err // unauthenticated calls are rejected upstream
			}
			outcome := biz.OutcomeOK
			if err != nil {
				outcome = biz.OutcomeError
			}
			events.Record(ctx, biz.Event{
				Phase:   biz.PhaseConfigChange,
				Outcome: outcome,
				Detail: map[string]any{
					"operation":   tr.Operation(),
					"operator_id": op.ID,
					"operator":    op.Username,
				},
			})
			return reply, err
		}
	}
}

func isReadOnly(tr transport.Transporter) bool {
	if ht, ok := tr.(khttp.Transporter); ok {
		return ht.Request().Method == http.MethodGet
	}
	// gRPC: treat List*/Get* as reads.
	op := tr.Operation()
	i := strings.LastIndex(op, "/")
	method := op[i+1:]
	return strings.HasPrefix(method, "Get") || strings.HasPrefix(method, "List")
}
