package util_test

import (
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"strings"
	"testing"
)

func TestNewConsoleLogger(t *testing.T) {
	output := testutil.CaptureStdout(t, func() {
		log := util.NewConsoleLogger("debug")
		if !log.IsConsoleOnly() {
			t.Fatal("expected console-only logger")
		}
		log.Info("console info %d", 1)
		log.Debug("console debug %d", 2)
		log.Warn("console warn %d", 3)
		log.Close()
	})

	if !strings.Contains(output, "console info 1") {
		t.Fatalf("expected info output, got %q", output)
	}
	if !strings.Contains(output, "console debug 2") {
		t.Fatalf("expected debug output, got %q", output)
	}
	if !strings.Contains(output, "console warn 3") {
		t.Fatalf("expected warn output, got %q", output)
	}
}

func TestNilLoggerMethodsAreSafe(t *testing.T) {
	var log *util.Logger
	log.Close()
	log.Info("ignored")
	log.Debug("ignored")
	log.Warn("ignored")

	if log.IsConsoleOnly() {
		t.Fatal("nil logger should not report console-only")
	}
}
