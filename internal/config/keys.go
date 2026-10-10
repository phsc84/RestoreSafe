package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	restoresafe "github.com/phsc84/restoresafe"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/security/cryptox"

	"go.yaml.in/yaml/v3"
)

// setting is a config.yaml key that has a default: its path ("argon2.time"
// for a key of a block) and the default as YAML.
type setting struct {
	key, value string
}

// settings are the keys with a default, in the order of config-SAMPLE.yaml.
// The values are the defaults Load applies to a missing key, which aren't
// always the sample's values (the sample keeps 3 chains, the default all).
var settings = []setting{
	{"split_size_mb", strconv.FormatInt(DefaultSplitSizeMB, 10)},
	{"retention_keep", "0"},
	{"differential.enabled", "true"},
	{"differential.full_backup_interval_days", strconv.Itoa(DefaultFullBackupIntervalDays)},
	{"differential.max_size_percent", strconv.Itoa(DefaultMaxSizePercent)},
	{"differential.retention_keep_differentials", "0"},
	{"exclude", "[]"},
	{"on_unreadable_file", strconv.Quote(OnUnreadableFail)},
	{"log_level", `"info"`},
	{"verify_after_backup", "false"},
	{"reminder_days", strconv.Itoa(DefaultReminderDays)},
	{"authentication_mode", strconv.Itoa(int(AuthModePassword))},
	{"yubikey_spare", "false"},
	{"recovery_code", "false"},
	{"password_min_length", strconv.Itoa(DefaultPasswordMinLength)},
	{"argon2.time", strconv.Itoa(int(cryptox.DefaultArgon2Params.Time))},
	{"argon2.memory_mb", strconv.Itoa(int(cryptox.DefaultArgon2Params.MemoryKB / 1024))},
	{"argon2.threads", strconv.Itoa(int(cryptox.DefaultArgon2Params.Threads))},
}

// requiredKeys have no default; validate rejects a file without them.
var requiredKeys = []string{"source_directories", "backup_directory"}

// blockKeys returns the keys of a block ("differential") in settings, without
// the block's name.
func blockKeys(block string) []string {
	var out []string
	for _, s := range settings {
		if b, key, ok := strings.Cut(s.key, "."); ok && b == block {
			out = append(out, key)
		}
	}
	return out
}

// topMapping returns the top-level mapping of a parsed file, nil for an
// empty file or one whose top level isn't a mapping.
func topMapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	return doc.Content[0]
}

// lookup returns the key and value nodes of key in mapping m; nil when m
// isn't a mapping or has no such key.
func lookup(m *yaml.Node, key string) (k, v *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

// missingKeys lists the settings that root lacks, in the order of settings:
// a block missing entirely by its name, otherwise its single keys by path.
func missingKeys(root *yaml.Node) []string {
	var out []string
	seen := make(map[string]bool)
	for _, s := range settings {
		block, _, nested := strings.Cut(s.key, ".")
		if !nested {
			if k, _ := lookup(root, s.key); k == nil {
				out = append(out, s.key)
			}
			continue
		}
		if seen[block] {
			continue
		}
		seen[block] = true
		_, b := lookup(root, block)
		keys := blockKeys(block)
		var missing []string
		for _, key := range keys {
			if k, _ := lookup(b, key); k == nil {
				missing = append(missing, block+"."+key)
			}
		}
		if len(missing) == len(keys) {
			out = append(out, block)
		} else {
			out = append(out, missing...)
		}
	}
	return out
}

// unknownField is the error yaml reports for a key that Config doesn't have.
var unknownField = regexp.MustCompile(`^line (\d+): field (.+) not found in type config\.(\w+)$`)

// blockOfType maps the Go types of the blocks to their config.yaml names.
var blockOfType = map[string]string{"Config": "", "Differential": "differential", "Argon2": "argon2"}

// unknownKeysError rewrites the error of a file with unknown keys, nil when
// err isn't only about unknown keys.
func unknownKeysError(err error) error {
	var te *yaml.TypeError
	if !errors.As(err, &te) || len(te.Errors) == 0 {
		return nil
	}
	var found []string
	for _, e := range te.Errors {
		m := unknownField.FindStringSubmatch(e)
		if m == nil {
			return nil
		}
		key := m[2]
		if block := blockOfType[m[3]]; block != "" {
			key = block + "." + key
		}
		found = append(found, fmt.Sprintf("'%s' (line %s)", key, m[1]))
	}
	const remedy = "Check the spelling, or remove the line; config-SAMPLE.yaml describes every setting."
	if len(found) == 1 {
		return problem.Errorf("Config file has an unknown setting: %s.", found[0]).WithRemedyOnOwnLine(remedy)
	}
	return problem.Errorf("Config file has unknown settings: %s.", strings.Join(found, ", ")).WithRemedyOnOwnLine(remedy)
}

// AddMissing adds the settings that the configuration file at path lacks,
// with their default values and their explanation from config-SAMPLE.yaml
// (GUI spec ST-10). It first saves a copy of the file next to it and returns the
// copy's path; "" when nothing was missing. The new file is written only when
// it loads, lacks nothing and gives the same configuration as before.
func AddMissing(path string, now time.Time) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("Config file can't be read: %w", err)
	}
	before, err := parse(data)
	if err != nil {
		return "", err
	}
	if len(before.MissingKeys) == 0 {
		return "", nil
	}
	text, err := withMissing(data, now)
	if err != nil {
		return "", err
	}
	after, err := parse(text)
	if err == nil && len(after.MissingKeys) > 0 {
		err = fmt.Errorf("still missing: %s", strings.Join(after.MissingKeys, ", "))
	}
	if err == nil && !reflect.DeepEqual(effective(before), effective(after)) {
		err = errors.New("the values would change")
	}
	if err != nil {
		return "", problem.Errorf("RestoreSafe couldn't add the missing settings to %s: %v.", path, err).WithRemedyOnOwnLine("Add them by hand; config-SAMPLE.yaml describes every setting.")
	}

	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	backup := path + "." + now.Format("2006-01-02_150405") + ".bak"
	if err := os.WriteFile(backup, data, mode); err != nil {
		return "", problem.Errorf("The copy of the config file can't be saved: %w", err).WithRemedyOnOwnLine("Check that you may write to the folder of the config file.")
	}
	if err := replaceFile(path, text, mode); err != nil {
		return "", problem.Errorf("The config file can't be written: %w", err).WithRemedyOnOwnLine("Check that the file isn't read-only, or add the settings by hand.")
	}
	return backup, nil
}

// replaceFile writes data to a temporary file next to path and renames it
// over path, so path is either the old or the new file, never a part.
func replaceFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name) //nolint:errcheck // best effort; the error that matters is err
	}
	return err
}

// effective returns c with every default made explicit, so two
// configurations compare equal when they do the same.
func effective(c *Config) Config {
	e := *c
	e.MissingKeys, e.ExcludeMatcher, e.Argon2Notices = nil, nil, nil
	if len(e.Exclude) == 0 {
		e.Exclude = nil
	}
	days := c.ReminderLimit()
	e.ReminderDays = &days
	enabled := c.Differential.IsEnabled()
	e.Differential.Enabled = &enabled
	e.Differential.FullBackupIntervalDays = c.Differential.IntervalDays()
	e.Differential.MaxSizePercent = c.Differential.SizePercent()
	return e
}

// withMissing returns data with the missing settings added: keys of a block
// that exists at the end of that block, everything else at the end of the
// file. The lines of data are kept as they are.
func withMissing(data []byte, now time.Time) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("Config file is invalid: %w", err)
	}
	root := topMapping(&doc)
	flow := problem.New("Config file is written in YAML flow style ({ … }), so RestoreSafe can't add settings to it.").WithRemedyOnOwnLine("Add them by hand; config-SAMPLE.yaml describes every setting.")
	if root == nil || root.Style&yaml.FlowStyle != 0 {
		return nil, flow
	}
	comments, err := sampleComments()
	if err != nil {
		return nil, err
	}
	value := make(map[string]string, len(settings))
	for _, s := range settings {
		value[s.key] = s.value
	}
	entry := func(key, indent string) []string {
		_, name, nested := strings.Cut(key, ".")
		if !nested {
			name = key
		}
		var out []string
		for _, c := range comments[key] {
			out = append(out, indent+strings.TrimSpace(c))
		}
		return append(out, indent+name+": "+value[key])
	}

	inserts := make(map[int][]string) // after line n (1-based) of data
	var tail [][]string
	for _, key := range missingKeys(root) {
		block, _, nested := strings.Cut(key, ".")
		keys := blockKeys(key)
		switch {
		case nested:
			_, b := lookup(root, block)
			if b.Style&yaml.FlowStyle != 0 {
				return nil, flow
			}
			indent := strings.Repeat(" ", b.Content[0].Column-1)
			after := lastLine(b)
			inserts[after] = append(inserts[after], entry(key, indent)...)
		case len(keys) > 0:
			k, v := lookup(root, key)
			if k != nil {
				// "differential:" without keys: they go right below it.
				if v.Kind != yaml.ScalarNode || v.Value != "" {
					return nil, flow
				}
				for _, child := range keys {
					inserts[k.Line] = append(inserts[k.Line], entry(key+"."+child, "  ")...)
				}
				continue
			}
			lines := entry(key, "")
			lines[len(lines)-1] = key + ":"
			for _, child := range keys {
				lines = append(lines, entry(key+"."+child, "  ")...)
			}
			tail = append(tail, lines)
		default:
			tail = append(tail, entry(key, ""))
		}
	}

	eol := "\n"
	if strings.Contains(string(data), "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var out []string
	for i, line := range lines {
		out = append(out, line)
		out = append(out, inserts[i+1]...)
	}
	if len(tail) > 0 {
		out = append(out, "", "# Settings added by RestoreSafe on "+now.Format("2006-01-02")+" with their default values.")
		for _, t := range tail {
			out = append(out, "")
			out = append(out, t...)
		}
	}
	return []byte(strings.Join(out, eol) + eol), nil
}

// lastLine returns the last line that n or any node below it starts on.
func lastLine(n *yaml.Node) int {
	last := n.Line
	for _, c := range n.Content {
		last = max(last, lastLine(c))
	}
	return last
}

// sampleComments returns the comment lines directly above each key of
// config-SAMPLE.yaml, by key path, as they are in the sample.
func sampleComments() (map[string][]string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(restoresafe.ConfigSample, &doc); err != nil {
		return nil, fmt.Errorf("config-SAMPLE.yaml is invalid: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(restoresafe.ConfigSample), "\r\n", "\n"), "\n")
	above := func(line int) []string {
		i := line - 1 // the key's index in lines
		start := i
		for start > 0 && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "#") {
			start--
		}
		return lines[start:i]
	}
	out := make(map[string][]string)
	root := topMapping(&doc)
	if root == nil {
		return nil, errors.New("config-SAMPLE.yaml is invalid: its top level isn't a mapping")
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		out[k.Value] = above(k.Line)
		if v.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(v.Content); j += 2 {
				out[k.Value+"."+v.Content[j].Value] = above(v.Content[j].Line)
			}
		}
	}
	return out, nil
}
