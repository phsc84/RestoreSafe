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
		{"foo:\n  bar: 1\n", "an unknown setting: 'foo' (line 4)."},
	} {
		_, err := loadConfigText(t, tc.extra)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "\nRemedy: Check the spelling") {
			t.Errorf("%q: got %v, want an error with %s", tc.extra, err, tc.want)
		}
	}
}

func TestLoadIgnoresRetiredKeys(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfigText(t, "io_diagnostics: false\nretention_keep: 4\nyubikey_spare: true\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Retired{{"io_diagnostics", 4}, {"yubikey_spare", 6}}
	if !slices.Equal(cfg.RetiredKeys, want) || cfg.RetentionKeep != 4 {
		t.Fatalf("retired %v, retention_keep %d", cfg.RetiredKeys, cfg.RetentionKeep)
	}
	if got := cfg.RetiredKeys[1].String(); got != "yubikey_spare (line 6)" {
		t.Fatalf("String: %q", got)
	}

	// An unknown key is still an error, and the error names only it.
	_, err = loadConfigText(t, "yubikey_spare: true\nfoo: 1\n")
	if err == nil || !strings.Contains(err.Error(), "an unknown setting: 'foo' (line 5).") || strings.Contains(err.Error(), "yubikey_spare") {
		t.Fatalf("got %v", err)
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
		"verify_after_backup", "reminder_days", "authentication_mode",
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

func TestUpdateAddsMissing(t *testing.T) {
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
			backup, err := Update(path, now)
			if err != nil {
				t.Fatalf("Update: %v", err)
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
			if again, err := Update(path, now); again != "" || err != nil {
				t.Fatalf("second run: %q, %v", again, err)
			}
		})
	}
}

func TestUpdateRefusesFlowStyle(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `{source_directories: ["C:/Docs"], backup_directory: "C:/Backup"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(path, time.Now()); err == nil || !strings.Contains(err.Error(), "flow style") {
		t.Fatalf("got %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != content {
		t.Fatal("the file was changed")
	}
	if matches, _ := filepath.Glob(path + ".*"); len(matches) > 0 {
		t.Fatalf("left files %v", matches)
	}
}

func TestUpdateRemovesRetired(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	sample := strings.ReplaceAll(string(restoresafe.ConfigSample), "\r\n", "\n")
	mode := "authentication_mode: 1\n"
	if !strings.Contains(sample, mode) {
		t.Fatal("config-SAMPLE.yaml has no authentication_mode: 1")
	}
	// How config-SAMPLE.yaml of 2.0 had yubikey_spare.
	old := mode + "\n# Modes 2 and 3: register a second YubiKey as spare.\n# Default: false\nyubikey_spare: false\n"

	for name, tc := range map[string]struct{ content, want string }{
		"with comments":   {strings.Replace(sample, mode, old, 1), sample},
		"list value":      {strings.Replace(sample, mode, mode+"yubikey_spare:\n  - a\n\n  - b\n", 1), sample},
		"block scalar":    {strings.Replace(sample, mode, mode+"yubikey_spare: |\n  one\n\n  two\n", 1), sample},
		"crlf":            {strings.ReplaceAll(strings.Replace(sample, mode, old, 1), "\n", "\r\n"), strings.ReplaceAll(sample, "\n", "\r\n")},
		"last, no eol":    {sample + "\nio_diagnostics: true", sample},
		"two at the end":  {sample + "\nio_diagnostics: true\n\nyubikey_spare: false\n", sample},
		"block and blank": {minimalConfig + "yubikey_spare: true\n" + sample[strings.Index(sample, "split_size_mb"):], minimalConfig + sample[strings.Index(sample, "split_size_mb"):]},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := Load(path)
			if err != nil || len(before.RetiredKeys) == 0 {
				t.Fatalf("retired %v, %v", before, err)
			}
			if _, err := Update(path, now); err != nil {
				t.Fatalf("Update: %v", err)
			}
			if data, _ := os.ReadFile(path); string(data) != tc.want {
				t.Fatalf("got:\n%s", data)
			}
		})
	}
}

func TestUpdateRemovesAndAdds(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(minimalConfig+"yubikey_spare: false\nargon2:\n  time: 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(path, time.Now()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	cfg, err := Load(path)
	if err != nil || len(cfg.MissingKeys) > 0 || len(cfg.RetiredKeys) > 0 || cfg.Argon2.Time != 4 {
		t.Fatalf("after: %+v, %v", cfg, err)
	}
}

func TestUpdateFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := Update(filepath.Join(dir, "none.yaml"), time.Now()); err == nil || !strings.Contains(err.Error(), "can't be read") {
		t.Fatalf("no file: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(minimalConfig+"foo: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(path, time.Now()); err == nil || !strings.Contains(err.Error(), "'foo'") {
		t.Fatalf("unknown key: %v", err)
	}
}

func TestLoadRejectsInvalidYAML(t *testing.T) {
	t.Parallel()
	_, err := loadConfigText(t, "exclude: [\n")
	if err == nil || !strings.Contains(err.Error(), "Config file is invalid") || !strings.Contains(err.Error(), "Check YAML syntax") {
		t.Fatalf("got %v", err)
	}
}
