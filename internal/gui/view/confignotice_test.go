package view

import (
	"errors"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
)

func TestConfigNotice(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{SourceDirectories: []string{`C:\Docs`}}
	if n := ConfigNoticeOf(cfg, `C:\RestoreSafe\config.yaml`); n != nil {
		t.Fatalf("up to date: %+v", n)
	}

	for _, tc := range []struct {
		name    string
		missing []string
		retired []config.Retired
		want    []string
		not     []string
	}{
		{"missing", []string{"reminder_days", "recovery_code", "differential"}, nil,
			[]string{"Missing (defaults apply):\n• reminder_days\n• recovery_code\n• differential\n\nUpdate fixes"},
			[]string{"No longer used"}},
		{"retired", nil, []config.Retired{{Key: "yubikey_spare", Line: 101}},
			[]string{"No longer used (ignored):\n• yubikey_spare (line 101)\n\nUpdate fixes"},
			[]string{"Missing"}},
		{"both", []string{"argon2"}, []config.Retired{{Key: "io_diagnostics", Line: 4}, {Key: "yubikey_spare", Line: 9}},
			[]string{"No longer used (ignored):\n• io_diagnostics (line 4)\n• yubikey_spare (line 9)\n\nMissing (defaults apply):\n• argon2\n\n" +
				"Update fixes the file and keeps a copy of the old one. Not now asks again at the next start."}, nil},
	} {
		cfg.MissingKeys, cfg.RetiredKeys = tc.missing, tc.retired
		n := ConfigNoticeOf(cfg, `C:\RestoreSafe\work.yaml`)
		if n == nil || n.Instruction != "Update work.yaml?" || n.Update != "&Update work.yaml" || n.Later != "&Not now" {
			t.Fatalf("%s: %+v", tc.name, n)
		}
		for _, w := range tc.want {
			if !strings.Contains(n.Content, w) {
				t.Errorf("%s: %q lacks %q", tc.name, n.Content, w)
			}
		}
		for _, w := range tc.not {
			if strings.Contains(n.Content, w) {
				t.Errorf("%s: %q has %q", tc.name, n.Content, w)
			}
		}
		checkWriting(t, n)
	}
}

func TestConfigUpdated(t *testing.T) {
	t.Parallel()
	done := ConfigUpdated(`C:\RestoreSafe\config.yaml`, `C:\RestoreSafe\config.yaml.2026-10-17_090000.bak`)
	if done.Instruction != "config.yaml is up to date" || done.Content != "The previous file is saved as config.yaml.2026-10-17_090000.bak." {
		t.Fatalf("updated: %+v", done)
	}
	failed := ConfigNotUpdated(`C:\RestoreSafe\config.yaml`, errors.New("the file is read-only"))
	if failed.Instruction != "config.yaml wasn't changed" || !strings.Contains(failed.Content, "read-only") || !strings.HasSuffix(failed.Content, "RestoreSafe uses the settings as they are.") {
		t.Fatalf("failed: %+v", failed)
	}
	checkWriting(t, []Confirm{done, failed})
}
