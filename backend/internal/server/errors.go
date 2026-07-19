// Typed API errors and the response envelope required by
// contracts/admin-api.md: {success, data, error, meta?}.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-kratos/kratos/v2/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	v1 "github.com/menta2k/universe/backend/api/netboot/v1"
)

func reason(r v1.ErrorReason) string { return v1.ErrorReason_name[int32(r)] }

// ErrValidation returns a 422 with per-field details.
func ErrValidation(msg string, fields map[string]string) *errors.Error {
	return errors.New(http.StatusUnprocessableEntity, reason(v1.ErrorReason_VALIDATION_FAILED), msg).
		WithMetadata(fields)
}

func ErrNotFound(what string) *errors.Error {
	return errors.New(http.StatusNotFound, reason(v1.ErrorReason_NOT_FOUND), what+" not found")
}

func ErrConflict(msg string) *errors.Error {
	return errors.New(http.StatusConflict, reason(v1.ErrorReason_CONFLICT), msg)
}

func ErrUnauthenticated() *errors.Error {
	return errors.New(http.StatusUnauthorized, reason(v1.ErrorReason_UNAUTHENTICATED), "authentication required")
}

func ErrPermissionDenied(msg string) *errors.Error {
	return errors.New(http.StatusForbidden, reason(v1.ErrorReason_PERMISSION_DENIED), msg)
}

func ErrDhcpDisabled() *errors.Error {
	return errors.New(http.StatusPreconditionFailed, reason(v1.ErrorReason_DHCP_DISABLED),
		"DHCP service is disabled; enable it before provisioning")
}

func ErrTooManyRequests() *errors.Error {
	return errors.New(http.StatusTooManyRequests, "RATE_LIMITED", "too many requests")
}

type envelopeError struct {
	Reason  string            `json:"reason"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   *envelopeError  `json:"error,omitempty"`
}

// UseProtoNames keeps field names snake_case, matching frontend/src/api/types.ts.
var protoJSON = protojson.MarshalOptions{EmitUnpopulated: true, UseProtoNames: true}

// ResponseEncoder wraps successful replies in the standard envelope.
func ResponseEncoder(w http.ResponseWriter, _ *http.Request, v any) error {
	var raw json.RawMessage
	var err error
	switch m := v.(type) {
	case nil:
		raw = nil
	case proto.Message:
		raw, err = protoJSON.Marshal(m)
	default:
		raw, err = json.Marshal(v)
	}
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(envelope{Success: true, Data: raw})
}

// ErrorEncoder writes errors in the standard envelope, hiding internals for
// unexpected failures (no sensitive detail leaks; Constitution V).
func ErrorEncoder(w http.ResponseWriter, _ *http.Request, err error) {
	se := errors.FromError(err)
	msg := se.Message
	if se.Code >= 500 {
		msg = "internal error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(int(se.Code))
	_ = json.NewEncoder(w).Encode(envelope{
		Success: false,
		Error: &envelopeError{
			Reason:  se.Reason,
			Message: msg,
			Details: se.Metadata,
		},
	})
}
