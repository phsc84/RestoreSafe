// Package plan decides what a backup run will do, without a password: which
// source directories it backs up under which names, whether each gets a full
// or a differential backup, whether existing keys are reused, and which
// backup sets retention removes afterwards. The backup workflow acts on these
// decisions; the health check and the user interface show them, so what is
// shown is what happens.
package plan
