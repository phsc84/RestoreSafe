package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Colors of the report view (RTF color table indexes). The marker symbol
// carries the meaning; color only supports it.
const (
	colorOK    = 1 // green
	colorInfo  = 2 // blue
	colorWarn  = 3 // amber
	colorError = 4 // red
	colorMuted = 5 // gray
)

const rtfColorTable = `{\colortbl ;\red16\green124\blue16;\red0\green95\blue184;\red178\green98\blue0;\red196\green30\blue30;\red100\green100\blue100;}`

// statusMarker returns the symbol and color index of a status.
func statusMarker(s interact.Status) (string, int) {
	switch s {
	case interact.StatusOK:
		return "✔", colorOK
	case interact.StatusInfo:
		return "ℹ", colorInfo
	case interact.StatusWarn:
		return "⚠", colorWarn
	case interact.StatusError:
		return "✖", colorError
	}
	return "", 0
}

// Indents in twips (1/1440 inch).
const (
	indentItem   = 200
	indentText   = 520 // after the marker
	indentDetail = 820
	twipsPerChar = 110 // rough width of a character at the base font size
)

// reportRTF renders r as RTF for the report view: the title in larger bold
// type, headings bold, items with a colored status marker, details indented,
// fields in two columns, and the issues at the end. fontPt is the base font
// size in points.
func reportRTF(r interact.Report, fontFace string, fontPt int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{\rtf1\ansi\deff0{\fonttbl{\f0\fswiss %s;}}%s\fs%d `, rtfEscape(fontFace), rtfColorTable, fontPt*2)
	if r.Title != "" {
		fmt.Fprintf(&b, `\pard\sb80\sa120{\b\fs%d %s}\par `, fontPt*2+6, rtfEscape(r.Title))
	}
	for i, section := range r.Sections {
		if i > 0 {
			b.WriteString(`\pard\par `)
		}
		fieldTab := indentItem
		for _, row := range section.Rows {
			if row.Kind == interact.RowField {
				if w := indentItem + (utf8.RuneCountInString(row.Label)+2)*twipsPerChar; w > fieldTab {
					fieldTab = w
				}
			}
		}
		for _, row := range section.Rows {
			writeRTFRow(&b, row, fieldTab)
		}
	}
	if len(r.Issues) > 0 {
		b.WriteString(`\pard\par `)
		for _, issue := range r.Issues {
			writeRTFItem(&b, issue.Status, issue.Text, nil, 0)
		}
	}
	b.WriteString("}")
	return b.String()
}

func writeRTFRow(b *strings.Builder, row interact.Row, fieldTab int) {
	switch row.Kind {
	case interact.RowHeading:
		fmt.Fprintf(b, `\pard\sb60{\b %s}\par `, rtfEscape(row.Label))
	case interact.RowField:
		fmt.Fprintf(b, `\pard\tx%d{\cf%d %s}\tab %s\par `, fieldTab, colorMuted, rtfEscape(row.Label), rtfEscape(row.Text))
	case interact.RowNote:
		fmt.Fprintf(b, `\pard %s\par `, rtfEscape(row.Text))
	case interact.RowItem:
		writeRTFItem(b, row.Status, row.Text, row.Details, indentItem)
	}
}

// writeRTFItem writes a status marker and text with a hanging indent, so
// wrapped lines align with the text, followed by the details.
func writeRTFItem(b *strings.Builder, status interact.Status, text string, details []string, indent int) {
	marker, color := statusMarker(status)
	textIndent := indent + indentText - indentItem
	fmt.Fprintf(b, `\pard\li%d\fi-%d\tx%d`, textIndent, textIndent-indent, textIndent)
	if marker != "" {
		fmt.Fprintf(b, `{\cf%d %s}`, color, rtfEscape(marker))
	}
	fmt.Fprintf(b, `\tab %s\par `, rtfEscape(text))
	for _, d := range details {
		fmt.Fprintf(b, `\pard\li%d{\cf%d %s}\par `, textIndent+indentDetail-indentText, colorMuted, rtfEscape("→ "+d))
	}
}

// rtfEscape escapes RTF control characters and writes non-ASCII characters
// as \u escapes (UTF-16 code units, signed, with "?" as ANSI fallback).
func rtfEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\' || r == '{' || r == '}':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\line `)
		case r == '\t':
			b.WriteString(`\tab `)
		case r < 0x80:
			b.WriteRune(r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%d?`, int16(r))
		default:
			r -= 0x10000
			fmt.Fprintf(&b, `\u%d?\u%d?`, int16(0xD800+(r>>10)), int16(0xDC00+(r&0x3FF)))
		}
	}
	return b.String()
}

// logRTF renders log lines as RTF for the log window: monospaced, warnings
// in amber, errors in red, a note (no warnings) muted; the text itself
// carries WARN and ERROR.
func logRTF(lines []view.LogLine, fontPt int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{\rtf1\ansi\deff0{\fonttbl{\f0\fmodern Consolas;}}%s\fs%d `, rtfColorTable, fontPt*2)
	for _, l := range lines {
		color := 0
		switch l.Tone {
		case view.ToneWarning:
			color = colorWarn
		case view.ToneError:
			color = colorError
		case view.ToneSecondary:
			color = colorMuted
		}
		fmt.Fprintf(&b, `{\cf%d %s}\par `, color, rtfEscape(l.Text))
	}
	b.WriteString("}")
	return b.String()
}
