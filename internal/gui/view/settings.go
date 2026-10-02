package view

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/workflow/health"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"strings"
)

// Setting is one value of a Settings card: a label, the value in words,
// and the config.yaml key behind it (tooltip, spec section 10).
type Setting struct {
	Label, Value string
	Key          string
	Tone         Tone
	Glyph        Glyph
}

// FolderSetting is a folder of the Folders card (ST-3).
type FolderSetting struct {
	Name, Path string
	Status     string
	Tone       Tone
	Glyph      Glyph
}

// SettingsCard is a card of settings.
type SettingsCard struct {
	Title string
	Rows  []Setting
	// Note is a line under the rows, "" for none, in NoteTone.
	Note     string
	NoteTone Tone
	// More are rows behind the link MoreLink ("Key derivation (Argon2id)").
	MoreLink string
	More     []Setting
	MoreNote string
}

// SettingsPage is the Settings page (spec 10): read-only in 2.0.0.
type SettingsPage struct {
	Title string
	// The configuration file card (ST-1, ST-2).
	ConfigTitle  string
	ConfigPath   string
	Edit, Reload Button
	ConfigNote   string
	// ConfigError is why the last Reload failed; the previous configuration
	// stays in use.
	ConfigError string

	FoldersTitle string
	Folders      []FolderSetting
	FolderRows   []Setting

	BackupDirTitle string
	BackupDir      string
	BackupDirState Setting
	BackupDirRows  []Setting
	Open           Button

	Differential, Retention, Checks, Keys, Logging SettingsCard
}

// SettingsOf computes the Settings page from the configuration in use, its
// file, the snapshot (nil while the first check runs), the error of the
// last Reload and whether an operation runs (Reload waits for it).
func SettingsOf(cfg *config.Config, configPath, backupDir string, s *health.Snapshot, reloadErr error, busy bool) SettingsPage {
	p := SettingsPage{
		Title:          navSettings,
		ConfigTitle:    settingsConfigFile,
		ConfigPath:     Path(configPath),
		Edit:           Button{Text: buttonEditConfigFile, Action: ActionEditConfig, Enabled: true},
		Reload:         Button{Text: buttonReload, Action: ActionReload, Enabled: !busy},
		ConfigNote:     settingsReloadNote,
		FoldersTitle:   settingsFolders,
		BackupDirTitle: settingsBackupDir,
		BackupDir:      Path(backupDir),
		Open:           Button{Text: buttonOpenFolder, Action: ActionOpenBackupDir, Enabled: true},
	}
	if busy {
		p.Reload.Reason = reasonReloadBusy
	}
	if reloadErr != nil {
		p.ConfigError = issueText(strings.ReplaceAll(reloadErr.Error(), "\nRemedy: ", " Remedy: "))
	}
	p.Folders = folderSettings(cfg, s)
	exclude := settingNothing
	if len(cfg.Exclude) > 0 {
		exclude = strings.Join(cfg.Exclude, ", ")
	}
	unreadable := unreadableStop
	if cfg.SkipUnreadableFiles() {
		unreadable = unreadableSkip
	}
	p.FolderRows = []Setting{
		{Label: settingLeaveOut, Value: exclude, Key: "exclude"},
		{Label: settingUnreadable, Value: unreadable, Key: "on_unreadable_file"},
	}
	p.BackupDirState = backupDirState(s)
	p.BackupDirRows = []Setting{{Label: settingSplit, Value: fmt.Sprintf(splitAt, Size(cfg.SplitSizeMB<<20)), Key: "split_size_mb"}}

	d := cfg.Differential
	p.Differential = SettingsCard{Title: cardDifferential, Rows: []Setting{{Label: settingDiffs, Value: onOff(d.IsEnabled()), Key: "differential.enabled"}}}
	if d.IsEnabled() {
		p.Differential.Rows = append(p.Differential.Rows,
			Setting{Label: settingFullAfter, Value: fmt.Sprintf(daysCount, d.IntervalDays()), Key: "differential.full_backup_interval_days"},
			Setting{Label: settingOrWhen, Value: fmt.Sprintf(diffReaches, d.SizePercent()), Key: "differential.max_size_percent"})
	}

	p.Retention = SettingsCard{Title: cardRetention, Note: retentionRuns, NoteTone: ToneSecondary}
	switch {
	case cfg.RetentionKeep <= 0 && d.RetentionKeepDifferentials <= 0:
		p.Retention.Rows = []Setting{{Label: settingKeep, Value: keepEverything, Key: "retention_keep"}}
	default:
		chains := keepAllChains
		if cfg.RetentionKeep > 0 {
			chains = fmt.Sprintf(keepNChains, chainCount(cfg.RetentionKeep))
		}
		diffs := keepAllDiffs
		if d.RetentionKeepDifferentials > 0 {
			diffs = fmt.Sprintf(keepNDiffs, d.RetentionKeepDifferentials)
		}
		p.Retention.Rows = []Setting{
			{Label: settingKeep, Value: chains, Key: "retention_keep"},
			{Label: settingKeepDiffs, Value: diffs, Key: "differential.retention_keep_differentials"},
		}
	}

	reminder := settingOff
	if n := cfg.ReminderLimit(); n > 0 {
		reminder = fmt.Sprintf(reminderAfter, n)
	}
	p.Checks = SettingsCard{Title: cardChecks, Rows: []Setting{
		{Label: settingVerifyAfter, Value: onOff(cfg.VerifyAfterBackup), Key: "verify_after_backup"},
		{Label: settingReminder, Value: reminder, Key: "reminder_days"},
	}}

	p.Keys = SettingsCard{Title: cardKeysUnlock, Rows: []Setting{{Label: settingUnlock, Value: capitalize(cfg.AuthenticationMode.Label()), Key: "authentication_mode"}}}
	if cfg.UseYubiKey() {
		p.Keys.Rows = append(p.Keys.Rows, Setting{Label: settingSpare, Value: onOff(cfg.YubiKeySpare), Key: "yubikey_spare"})
	}
	p.Keys.Rows = append(p.Keys.Rows, Setting{Label: settingRecovery, Value: onOff(cfg.RecoveryCode), Key: "recovery_code"})
	if !cfg.IsYubiKeyOnly() {
		p.Keys.Rows = append(p.Keys.Rows, Setting{Label: settingPwLength, Value: fmt.Sprintf(atLeastChars, cfg.PasswordMinLength), Key: "password_min_length"})
	}
	p.Keys.MoreLink = linkArgon2
	p.Keys.More = []Setting{
		{Label: settingArgonTime, Value: fmt.Sprintf("%d", cfg.Argon2.Time), Key: "argon2.time"},
		{Label: settingArgonMemory, Value: Size(int64(cfg.Argon2.MemoryMB) << 20), Key: "argon2.memory_mb"},
		{Label: settingArgonThreads, Value: fmt.Sprintf("%d", cfg.Argon2.Threads), Key: "argon2.threads"},
	}
	p.Keys.MoreNote = argon2Note
	if s != nil && s.Keys.NewKeysReason != "" {
		p.Keys.Note, p.Keys.NoteTone = fmt.Sprintf(keysNewNeeded, s.Keys.NewKeysReason), ToneInfo
	}

	p.Logging = SettingsCard{Title: cardLogging, Rows: []Setting{
		{Label: settingLogLevel, Value: capitalize(cfg.LogLevel), Key: "log_level"},
		{Label: settingIODiag, Value: onOff(cfg.IODiagnostics), Key: "io_diagnostics"},
	}}
	return p
}

// folderSettings lists the configured folders with their state from the
// check (ST-3).
func folderSettings(cfg *config.Config, s *health.Snapshot) []FolderSetting {
	if s == nil {
		var out []FolderSetting
		for _, d := range cfg.SourceDirectories {
			out = append(out, FolderSetting{Path: Path(d), Status: statusChecking, Tone: ToneSecondary})
		}
		return out
	}
	var out []FolderSetting
	for _, f := range s.Folders {
		row := FolderSetting{Name: f.BackupName, Path: Path(f.Resolved), Status: folderFound, Tone: ToneSuccess, Glyph: GlyphCheck}
		switch {
		case f.Err != nil:
			row.Status, row.Tone, row.Glyph = folderUnreadable, ToneError, GlyphError
			if p := folderProblem(s, f.BackupName); p != nil && p.Code == interact.CodeSourceMissing {
				row.Status = folderNotFound
			}
		case f.Skip:
			row.Status, row.Tone, row.Glyph = folderDuplicate, ToneSecondary, GlyphNone
		}
		out = append(out, row)
	}
	return out
}

// backupDirState is the backup directory's reachability and free space
// (ST-4).
func backupDirState(s *health.Snapshot) Setting {
	if s == nil {
		return Setting{Value: statusChecking, Tone: ToneSecondary}
	}
	for _, p := range s.Problems {
		switch p.Code {
		case interact.CodeBackupDirUnreachable:
			return Setting{Value: dirUnreachable, Tone: ToneError, Glyph: GlyphError}
		case interact.CodeBackupDirNotWritable:
			return Setting{Value: dirNotWritable, Tone: ToneError, Glyph: GlyphError}
		}
	}
	if s.Storage.Known {
		return Setting{Value: fmt.Sprintf(dirReachableFree, Size(s.Storage.FreeBytes)), Tone: ToneSuccess, Glyph: GlyphCheck}
	}
	return Setting{Value: dirReachable, Tone: ToneSuccess, Glyph: GlyphCheck}
}

func onOff(on bool) string {
	if on {
		return settingOn
	}
	return settingOff
}
