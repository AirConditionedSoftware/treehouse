//go:build !darwin && !linux

package cmd

import (
	"errors"
	"io/fs"
)

// cloneFile on platforms without a copy-on-write file clone: always
// unsupported, so every copy takes the streamed path.
func cloneFile(src, dst string, perm fs.FileMode) error {
	return errors.ErrUnsupported
}
