package integration

import (
	"strings"
	"testing"

	"github.com/menta2k/universe/backend/tests/integration/testenv"
)

// TestProvisionFlowOverHTTP drives profile + machine + provision + cancel over
// the real HTTP API, exercising the machine usecase provisioning paths, session
// creation, profile lifecycle, and the DHCP enable precondition end-to-end.
func TestProvisionFlowOverHTTP(t *testing.T) {
	env := testenv.Start(t)
	base := startFullServer(t, env)
	jar := login(t, base)

	// Create a profile via the API.
	profileBody := `{"name":"noble-web","ubuntu_release":"noble","storage_layout":"{\"mode\":\"lvm\"}","ssh_authorized_keys":["ssh-ed25519 AAAA test@host"],"packages":["nginx"]}`
	code, body, _ := doJSON(t, jar, "POST", base+"/api/v1/profiles", profileBody)
	if code != 200 {
		t.Fatalf("create profile: code=%d body=%s", code, body)
	}
	profileID := extractJSONField(t, body, "id")

	// Register a machine assigned to the profile.
	code, body, _ = doJSON(t, jar, "POST", base+"/api/v1/machines",
		`{"mac":"52:54:00:aa:00:01","name":"prov-node","profile_id":"`+profileID+`"}`)
	if code != 200 {
		t.Fatalf("create machine: code=%d body=%s", code, body)
	}
	machineID := extractJSONField(t, body, "id")

	// Provision before DHCP is enabled -> 412 DHCP_DISABLED.
	if code, body, _ := doJSON(t, jar, "POST", base+"/api/v1/machines/"+machineID+"/provision", "{}"); code != 412 {
		t.Fatalf("provision w/o dhcp: code=%d body=%s", code, body)
	}

	// Enable DHCP, then provision succeeds and opens a session.
	if code, _, _ := doJSON(t, jar, "POST", base+"/api/v1/dhcp/enable", "{}"); code != 200 {
		t.Fatalf("enable dhcp: %d", code)
	}
	code, body, _ = doJSON(t, jar, "POST", base+"/api/v1/machines/"+machineID+"/provision", "{}")
	if code != 200 || !strings.Contains(body, "installing") {
		t.Fatalf("provision: code=%d body=%s", code, body)
	}

	// GetMachine now reports an active session.
	if code, body, _ := doJSON(t, jar, "GET", base+"/api/v1/machines/"+machineID, ""); code != 200 || !strings.Contains(body, "active_session_id") {
		t.Fatalf("get machine: code=%d body=%s", code, body)
	}

	// Second provision conflicts (409).
	if code, _, _ := doJSON(t, jar, "POST", base+"/api/v1/machines/"+machineID+"/provision", "{}"); code != 409 {
		t.Errorf("double provision: expected 409, got %d", code)
	}

	// Delete blocked while installing (409).
	if code, _, _ := doJSON(t, jar, "DELETE", base+"/api/v1/machines/"+machineID, ""); code != 409 {
		t.Errorf("delete while installing: expected 409, got %d", code)
	}

	// Cancel finishes the session as failed but returns the machine to "ready" —
	// a clean, editable, re-armable state; "failed" is reserved for real install
	// failures. Then the session is gone and delete works.
	if code, body, _ := doJSON(t, jar, "POST", base+"/api/v1/machines/"+machineID+"/cancel", "{}"); code != 200 ||
		!strings.Contains(body, `"provision_state":"ready"`) {
		t.Fatalf("cancel: code=%d body=%s", code, body)
	}
	if code, _, _ := doJSON(t, jar, "DELETE", base+"/api/v1/machines/"+machineID, ""); code != 200 {
		t.Errorf("delete after cancel: %d", code)
	}

	// Profile update (version bump), clone, delete.
	upd := `{"profile":{"name":"noble-web","ubuntu_release":"noble","storage_layout":"{\"mode\":\"direct\"}","ssh_authorized_keys":["ssh-ed25519 AAAA test@host"]}}`
	if code, body, _ := doJSON(t, jar, "PUT", base+"/api/v1/profiles/"+profileID, upd); code != 200 || !strings.Contains(body, `"version":2`) {
		t.Errorf("profile update: code=%d body=%s", code, body)
	}
	if code, _, _ := doJSON(t, jar, "POST", base+"/api/v1/profiles/"+profileID+"/clone", `{"new_name":"noble-web-copy"}`); code != 200 {
		t.Errorf("clone: %d", code)
	}
	if code, _, _ := doJSON(t, jar, "POST", base+"/api/v1/profiles/"+profileID+"/preview", `{}`); code != 200 {
		t.Errorf("preview: %d", code)
	}
	if code, _, _ := doJSON(t, jar, "DELETE", base+"/api/v1/profiles/"+profileID, ""); code != 200 {
		t.Errorf("delete profile: %d", code)
	}
}

// extractJSONField pulls a top-level "field":"value" string out of the envelope
// data without a full struct (good enough for id extraction in tests).
func extractJSONField(t *testing.T, body, field string) string {
	t.Helper()
	marker := `"` + field + `":"`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("field %q not found in %s", field, body)
	}
	rest := body[i+len(marker):]
	end := strings.IndexByte(rest, '"')
	return rest[:end]
}
