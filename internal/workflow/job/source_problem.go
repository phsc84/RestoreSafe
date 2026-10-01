package job

import (
	"RestoreSafe/internal/workflow/interact"
	"errors"
	"io/fs"
)

// SourceProblemCode classifies the error of a source directory:
// CodeSourceMissing when it does not exist, CodeSourceInvalid otherwise.
func SourceProblemCode(err error) interact.Code {
	if errors.Is(err, fs.ErrNotExist) {
		return interact.CodeSourceMissing
	}
	return interact.CodeSourceInvalid
}
