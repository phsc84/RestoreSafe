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
		{"one missing", []string{"reminder_days"}, nil,
			[]string{"reminder_days isn't in work.yaml, so its default applies.", "then adds the missing settings with their default values and explanations."},
			[]string{"no longer used"}},
		{"three missing", []string{"reminder_days", "recovery_code", "differential"}, nil,
			[]string{"3 settings aren't in work.yaml, so their defaults apply: reminder_days, recovery_code, differential."}, nil},
		{"one retired", nil, []config.Retired{{Key: "yubikey_spare", Line: 101}},
			[]string{"yubikey_spare (line 101) is no longer used by this version of RestoreSafe and is ignored.", "then removes the settings that are no longer used."},
			[]string{"adds the missing"}},
		{"both", []string{"argon2"}, []config.Retired{{Key: "io_diagnostics", Line: 4}, {Key: "yubikey_spare", Line: 9}},
			[]string{"2 settings are no longer used by this version of RestoreSafe and are ignored: io_diagnostics (line 4), yubikey_spare (line 9).",
				"Update work.yaml saves a copy of the file next to it, then removes the settings that are no longer used and adds the missing settings",
				"RestoreSafe asks again at the next start."}, nil},
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
