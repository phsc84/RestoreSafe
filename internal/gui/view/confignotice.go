package view

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/phsc84/restoresafe/internal/config"
)

// ConfigNotice is the dialog at start and after Reload that names the
// settings the configuration file lacks and the ones it has that
// RestoreSafe no longer uses (GUI spec ST-10).
type ConfigNotice struct {
	Instruction, Content string
	// Update brings the file up to date; Later, the default for nothing,
	// keeps it as it is until the next start.
	Update, Later string
}

// ConfigNoticeOf returns the dialog for the configuration loaded from
// configPath, nil when the file has every setting and no retired one.
func ConfigNoticeOf(cfg *config.Config, configPath string) *ConfigNotice {
	if len(cfg.MissingKeys) == 0 && len(cfg.RetiredKeys) == 0 {
		return nil
	}
	name := filepath.Base(configPath)
	var paras []string
	// list is a heading with one bullet per setting.
	list := func(heading string, keys []string) string {
		lines := []string{heading}
		for _, k := range keys {
			lines = append(lines, bullet+k)
		}
		return strings.Join(lines, "\n")
	}
	if len(cfg.RetiredKeys) > 0 {
		keys := make([]string, len(cfg.RetiredKeys))
		for i, r := range cfg.RetiredKeys {
			keys[i] = r.String()
		}
		paras = append(paras, list(retiredHeading, keys))
	}
	if len(cfg.MissingKeys) > 0 {
		paras = append(paras, list(missingHeading, cfg.MissingKeys))
	}
	paras = append(paras, updateDoes)
	return &ConfigNotice{
		Instruction: fmt.Sprintf(updateInstruction, name),
		Content:     strings.Join(paras, "\n\n"),
		Update:      fmt.Sprintf(buttonUpdateConfig, name),
		Later:       buttonNotNow,
	}
}

// ConfigUpdated is the message after the file was brought up to date:
// where the copy of the previous file is. Yes is its only button.
func ConfigUpdated(configPath, copyPath string) Confirm {
	return Confirm{
		Instruction: fmt.Sprintf(updatedInstruction, filepath.Base(configPath)),
		Content:     fmt.Sprintf(updatedCopy, filepath.Base(copyPath)),
		Yes:         buttonOK,
	}
}

// ConfigNotUpdated is the message when the file couldn't be brought up to
// date; the configuration stays in use as it is.
func ConfigNotUpdated(configPath string, err error) Confirm {
	return Confirm{
		Instruction: fmt.Sprintf(notUpdatedInstruction, filepath.Base(configPath)),
		Content:     errorText(err) + "\n\n" + notUpdatedLine,
		Yes:         buttonOK,
	}
}
