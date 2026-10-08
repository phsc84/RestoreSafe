// Package catalog finds the backup sets in a backup directory and tells what
// state each is in: complete, incomplete (interrupted, missing parts),
// renamed or damaged, with its header and the part files it consists of. It
// finds the full backup of a differential and the current key set, groups
// the sets into backup runs, and lists the leftovers of interrupted backups
// and the files of RestoreSafe 1.x.
package catalog
