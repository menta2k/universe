package integration

import (
	"io"
	"log/slog"
)

// testLog returns a silent logger for integration tests.
func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
