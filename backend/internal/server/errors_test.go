package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/errors"
)

func TestErrorHelpersCarryReasonAndStatus(t *testing.T) {
	cases := []struct {
		name   string
		err    *errors.Error
		code   int
		reason string
	}{
		{"validation", ErrValidation("bad field", map[string]string{"mac": "invalid"}), 422, "VALIDATION_FAILED"},
		{"not found", ErrNotFound("machine"), 404, "NOT_FOUND"},
		{"conflict", ErrConflict("active session exists"), 409, "CONFLICT"},
		{"unauthenticated", ErrUnauthenticated(), 401, "UNAUTHENTICATED"},
		{"dhcp disabled", ErrDhcpDisabled(), 412, "DHCP_DISABLED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if int(tc.err.Code) != tc.code {
				t.Errorf("code = %d, want %d", tc.err.Code, tc.code)
			}
			if tc.err.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", tc.err.Reason, tc.reason)
			}
		})
	}
}

func TestErrorEncoderEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/machines/x", nil)
	ErrorEncoder(rec, req, ErrValidation("mac is invalid", map[string]string{"mac": "not a mac"}))

	if rec.Code != 422 {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var body struct {
		Success bool `json:"success"`
		Error   struct {
			Reason  string            `json:"reason"`
			Message string            `json:"message"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Success {
		t.Error("success must be false")
	}
	if body.Error.Reason != "VALIDATION_FAILED" || !strings.Contains(body.Error.Message, "mac") {
		t.Errorf("unexpected error payload: %+v", body.Error)
	}
	if body.Error.Details["mac"] == "" {
		t.Error("details lost in encoding")
	}
}

func TestResponseEncoderEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/machines", nil)
	if err := ResponseEncoder(rec, req, map[string]string{"id": "abc"}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var body struct {
		Success bool              `json:"success"`
		Data    map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !body.Success || body.Data["id"] != "abc" {
		t.Errorf("unexpected envelope: %s", rec.Body.String())
	}
}

func TestResponseEncoderNilData(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/machines/x", nil)
	if err := ResponseEncoder(rec, req, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}
