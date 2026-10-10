package job

import (
	"errors"
	"io/fs"

	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// SourceProblemCode classifies the error of a source directory:
// CodeSourceMissing when it does not exist, CodeSourceInvalid otherwise.
func SourceProblemCode(err error) interact.Code {
	if errors.Is(err, fs.ErrNotExist) {
		return interact.CodeSourceMissing
	}
	return interact.CodeSourceInvalid
}
