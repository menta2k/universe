package biz

import (
	"io"
	"log/slog"
)

// testLogger returns a silent logger for unit tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
