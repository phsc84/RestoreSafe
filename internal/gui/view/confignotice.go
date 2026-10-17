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
	var paras, does []string
	switch n := len(cfg.RetiredKeys); {
	case n == 1:
		paras = append(paras, fmt.Sprintf(retiredOne, cfg.RetiredKeys[0]))
	case n > 1:
		keys := make([]string, n)
		for i, r := range cfg.RetiredKeys {
			keys[i] = r.String()
		}
		paras = append(paras, fmt.Sprintf(retiredMany, n, strings.Join(keys, ", ")))
	}
	if len(cfg.RetiredKeys) > 0 {
		does = append(does, updateRemoves)
	}
	switch n := len(cfg.MissingKeys); {
	case n == 1:
		paras = append(paras, fmt.Sprintf(missingOne, cfg.MissingKeys[0], name))
	case n > 1:
		paras = append(paras, fmt.Sprintf(missingMany, n, name, strings.Join(cfg.MissingKeys, ", ")))
	}
	if len(cfg.MissingKeys) > 0 {
		does = append(does, updateAdds)
	}
	update := fmt.Sprintf(buttonUpdateConfig, name)
	paras = append(paras,
		fmt.Sprintf(updateDoes, strings.ReplaceAll(update, "&", ""), strings.Join(does, " and ")),
		fmt.Sprintf(updateLater, name))
	return &ConfigNotice{
		Instruction: fmt.Sprintf(updateInstruction, name),
		Content:     strings.Join(paras, "\n\n"),
		Update:      update,
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
