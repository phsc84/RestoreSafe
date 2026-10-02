package gui

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// TestScriptsKnowTheControlIDs keeps the control IDs the GUI test scripts
// use (scripts/gui-test/GuiDriver.ps1) equal to the code's: they are the
// AutomationIds of spec 15.
func TestScriptsKnowTheControlIDs(t *testing.T) {
	t.Parallel()
	ids := map[string]int{
		"Sidebar": idSidebar, "HeroPrimary": idHeroPrimary, "HeroSecondary": idHeroSecondary,
		"HeroTitle": idHeroTitle,
		"RunCancel": idRunCancel, "RunLog": idRunLog, "RunDetails": idRunDetails, "RunDone": idRunDone,
		"RunOpen": idRunOpen, "RunTitle": idRunTitle,
		"PlanStart": idPlanStart, "PlanFull": idPlanFull, "PlanNewKeys": idPlanNewKeys,
		"PlanCancel": idPlanCancel, "PlanDetails": idPlanDetails,
		"CredentialOK": idCredentialOK, "CredentialCancel": idCredentialCancel, "CredentialLink": idCredentialLink,
		"BackupsFilter": idBackupsFilter, "BackupsList": idBackupsList, "BackupsRestore": idBackupsRestore,
		"BackupsVerify": idBackupsVerify, "LogAll": idLogAll, "LogWarnings": idLogWarnings,
		"LogOpen": idLogOpen, "LogToggle": idLogToggle, "EmptyBackUp": idEmptyBackUp,
		"WizardBack": idWizBack, "WizardNext": idWizNext, "WizardCancel": idWizCancel,
		"WizardList": idWizList, "WizardDest": idWizDest, "WizardBrowse": idWizBrowse,
		"WizardBackupDir": idWizIntoBackupDir, "WizardDetails": idWizDetails,
		"SettingsEdit": idSettingsEdit, "SettingsReload": idSettingsReload,
		"SettingsOpen": idSettingsOpen, "SettingsMore": idSettingsMore,
	}
	data, err := os.ReadFile("../../scripts/gui-test/GuiDriver.ps1")
	if err != nil {
		t.Fatal(err)
	}
	table := regexp.MustCompile(`(?s)\$Ids = \[ordered\]@\{(.*?)\n\}`).FindSubmatch(data)
	if table == nil {
		t.Fatal("no $Ids table in GuiDriver.ps1")
	}
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*(\d+)\s*$`).FindAllSubmatch(table[1], -1) {
		name := string(m[1])
		n, _ := strconv.Atoi(string(m[2]))
		want, ok := ids[name]
		if !ok {
			t.Errorf("GuiDriver.ps1 names %s, which the test does not know", name)
			continue
		}
		if n != want {
			t.Errorf("%s is %d in GuiDriver.ps1, %d in the code", name, n, want)
		}
		seen[name] = true
	}
	for name := range ids {
		if !seen[name] {
			t.Errorf("GuiDriver.ps1 lacks %s", name)
		}
	}
}
