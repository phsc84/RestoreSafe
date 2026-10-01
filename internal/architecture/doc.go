// Package architecture holds no code; its test checks that the imports
// between RestoreSafe's packages point downward only.
//
// The layers, from top to bottom (a package may import its own layer and
// every layer below it):
//
//	cmd             restoresafe, yubidiag
//	gui             gui, gui/win32
//	workflow        interact, plan, backup, restore, verify, health, unlock, restorepoint, job
//	format          archive, container, manifest, catalog, naming, setwriter
//	config, logging config -> security/cryptox (Argon2 bounds); logging -> buildinfo
//	security        cryptox, recovery, yubikey (recovery and yubikey -> cryptox)
//	fsx, buildinfo  no internal imports
//
// format imports config (exclude patterns in archive and setwriter, the
// authentication mode in container, the configuration in catalog), so config
// sits below format, not beside it.
//
// Additional rules:
//
//   - No workflow package imports backup, restore, verify, or health; only the
//     frontend starts workflows.
//   - workflow/interact (and interacttest) imports no other workflow package:
//     the contract does not depend on workflow code.
//   - workflow/plan imports no other workflow package: backup and health
//     act on its decisions, so it sits below them.
//   - gui/win32 has no internal imports.
//   - testutil, e2e, and architecture are test support: exempt from the
//     layers, never imported by production code.
//
// The test fails for a package that belongs to no layer.
package architecture
