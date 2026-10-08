package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	restoresafe "github.com/phsc84/restoresafe"

	"go.yaml.in/yaml/v3"
)

const minimalConfig = "source_directories:\n  - \"C:/Users/Test/Documents\"\nbackup_directory: \"C:/Backup\"\n"

func TestLoadRejectsUnknownKeys(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ extra, want string }{
		{"retention_kep: 2\n", "'retention_kep' (line 4)"},
		{"argon2:\n  memroy_mb: 1024\n", "'argon2.memroy_mb' (line 5)"},
		{"differential:\n  enabled: true\n  interval: 3\n", "'differential.interval' (line 6)"},
		{"foo: 1\nbar: 2\n", "unknown settings: 'foo' (line 4), 'bar' (line 5)"},
	} {
		_, err := loadConfigText(t, tc.extra)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "\nRemedy: ") {
			t.Errorf("%q: got %v, want an error with %s", tc.extra, err, tc.want)
		}
	}
}

// TestEveryKeyIsKnown keeps settings, Config and config-SAMPLE.yaml in step:
// every key of the sample loads, every yaml key of Config is in settings or
// required, and settings follow the order of the sample.
func TestEveryKeyIsKnown(t *testing.T) {
	t.Parallel()

	var tags []string
	var walk func(prefix string, typ reflect.Type)
	walk = func(prefix string, typ reflect.Type) {
		for f := range typ.Fields() {
			tag := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			if f.Type.Kind() == reflect.Struct {
				walk(prefix+tag+".", f.Type)
				continue
			}
			tags = append(tags, prefix+tag)
		}
	}
	walk("", reflect.TypeFor[Config]())
	var known []string
	for _, s := range settings {
		known = append(known, s.key)
	}
	known = append(known, requiredKeys...)
	for _, tag := range tags {
		if !slices.Contains(known, tag) {
			t.Errorf("config key %s has neither a default in settings nor is it required", tag)
		}
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(restoresafe.ConfigSample, &doc); err != nil {
		t.Fatal(err)
	}
	var sample []string
	root := topMapping(&doc)
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i].Value, root.Content[i+1]
		if v.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(v.Content); j += 2 {
				sample = append(sample, k+"."+v.Content[j].Value)
			}
			continue
		}
		if !slices.Contains(requiredKeys, k) {
			sample = append(sample, k)
		}
	}
	var order []string
	for _, s := range settings {
		order = append(order, s.key)
	}
	if !slices.Equal(sample, order) {
		t.Errorf("settings %v differ from the keys of config-SAMPLE.yaml %v", order, sample)
	}

	comments, err := sampleComments()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range append(order, "differential", "argon2") {
		if !strings.Contains(key, ".") && len(comments[key]) == 0 {
			t.Errorf("config-SAMPLE.yaml has no explanation above %s", key)
		}
	}
}

func TestMissingKeys(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfigText(t, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"split_size_mb", "retention_keep", "differential", "exclude", "on_unreadable_file", "log_level",
		"io_diagnostics", "verify_after_backup", "reminder_days", "authentication_mode", "yubikey_spare",
		"recovery_code", "password_min_length", "argon2"}
	if !slices.Equal(cfg.MissingKeys, want) {
		t.Fatalf("minimal file: missing %v, want %v", cfg.MissingKeys, want)
	}

	cfg, err = loadConfigText(t, "differential:\n  enabled: false\n  max_size_percent: 40\nargon2:\n")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.MissingKeys, "differential.full_backup_interval_days") ||
		!slices.Contains(cfg.MissingKeys, "differential.retention_keep_differentials") ||
		slices.Contains(cfg.MissingKeys, "differential.enabled") || !slices.Contains(cfg.MissingKeys, "argon2") {
		t.Fatalf("partial blocks: missing %v", cfg.MissingKeys)
	}

	cfg, err = Load(filepath.Join("..", "..", "config-SAMPLE.yaml"))
	if err != nil || len(cfg.MissingKeys) > 0 {
		t.Fatalf("sample: missing %v, %v", cfg.MissingKeys, err)
	}
}

func TestAddMissing(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 4, 15, 30, 12, 0, time.UTC)

	for name, content := range map[string]string{
		"minimal":         minimalConfig,
		"partial blocks":  minimalConfig + "# my differential settings\ndifferential:\n    enabled: false # not now\n    max_size_percent: 40\nretention_keep: 4\nargon2:\n",
		"crlf":            strings.ReplaceAll(minimalConfig+"argon2:\n  time: 5\n", "\n", "\r\n"),
		"no final eol":    minimalConfig + "log_level: \"debug\"",
		"comments at end": minimalConfig + "differential:\n  enabled: true\n# the end\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			backup, err := AddMissing(path, now)
			if err != nil {
				t.Fatalf("AddMissing: %v", err)
			}
			if backup != path+".2026-10-04_153012.bak" {
				t.Fatalf("copy %s", backup)
			}
			if old, err := os.ReadFile(backup); err != nil || string(old) != content {
				t.Fatalf("copy differs from the old file: %v", err)
			}
			after, err := Load(path)
			if err != nil {
				t.Fatalf("the new file doesn't load: %v", err)
			}
			if len(after.MissingKeys) > 0 || !reflect.DeepEqual(effective(before), effective(after)) {
				t.Fatalf("missing %v; before %+v, after %+v", after.MissingKeys, effective(before), effective(after))
			}
			data, _ := os.ReadFile(path)
			text := string(data)
			crlf := strings.Contains(content, "\r\n")
			for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
				if !strings.Contains(text, line) {
					t.Fatalf("line %q of the old file is gone:\n%s", line, text)
				}
			}
			if crlf && strings.Count(text, "\n") != strings.Count(text, "\r\n") {
				t.Fatal("line endings changed")
			}
			if !strings.Contains(text, "# Settings added by RestoreSafe on 2026-10-04") || !strings.Contains(text, "# Default: 7  |  Minimum: 0  |  Maximum: 365") {
				t.Fatalf("explanations missing:\n%s", text)
			}
			if again, err := AddMissing(path, now); again != "" || err != nil {
				t.Fatalf("second run: %q, %v", again, err)
			}
		})
	}
}

func TestAddMissingRefusesFlowStyle(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `{source_directories: ["C:/Docs"], backup_directory: "C:/Backup"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AddMissing(path, time.Now()); err == nil || !strings.Contains(err.Error(), "flow style") {
		t.Fatalf("got %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != content {
		t.Fatal("the file was changed")
	}
	if matches, _ := filepath.Glob(path + ".*"); len(matches) > 0 {
		t.Fatalf("left files %v", matches)
	}
}
