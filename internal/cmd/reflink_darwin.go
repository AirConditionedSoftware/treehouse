//go:build darwin

package cmd

import (
	"io/fs"

	"golang.org/x/sys/unix"
)

// cloneFile clones src to dst with clonefile(2) — on APFS a metadata-only
// copy-on-write operation, so multi-gigabyte files land near-instantly.
// flags is 0: only regular files reach this, and streaming follows symlinks
// too, so there is no symlink behavior to preserve. perm is ignored —
// clonefile copies src's mode, and perm is src's mode at every call site.
// An existing dst fails with EEXIST and is deliberately not removed and
// retried: the streamed overwrite (O_TRUNC) keeps the existing dst's mode,
// which remove-and-clone would silently swap for src's.
func cloneFile(src, dst string, perm fs.FileMode) error {
	return unix.Clonefile(src, dst, 0)
}
