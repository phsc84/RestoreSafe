package view

// Tone is the meaning of a color; the page maps it to the palette.
type Tone int

const (
	ToneNeutral Tone = iota
	ToneSuccess
	ToneWarning
	ToneError
	ToneInfo
	ToneSecondary
)

// Glyph names an icon; the page maps it to the icon font.
type Glyph int

const (
	GlyphNone Glyph = iota
	GlyphCheck
	GlyphWarning
	GlyphError
	GlyphInfo
	GlyphShield
	GlyphFolder
	GlyphDrive
	GlyphKey
)

// Action is what a button or link does; the page maps it to a handler.
type Action int

const (
	ActionNone Action = iota
	ActionBackUp
	ActionCheckAgain
	ActionShowInBackups
	ActionEditConfig
	ActionOpenSettings
	ActionRestore
	ActionVerify
	ActionOpenBackupDir
	ActionStartBackup
	ActionFullBackup
	ActionAutomaticPlan
	ActionNewKeys
	ActionCancel
	ActionShowDetails
	ActionShowLog
	ActionDismiss
	ActionStartRestore
	ActionStartVerify
	ActionOpenRestored
	ActionReload
	ActionAddMissing
)

// Button is a button or link of a view.
type Button struct {
	Text    string
	Action  Action
	Enabled bool
	// Reason says why a disabled button is disabled (tooltip).
	Reason string
}

// BadgeKind is the kind of a type badge.
type BadgeKind int

const (
	BadgeFull BadgeKind = iota
	BadgeDiff
)

// Badge is a type badge: FULL, DIFF 3.
type Badge struct {
	Kind BadgeKind
	Text string
	// Name is what screen readers announce.
	Name string
}
