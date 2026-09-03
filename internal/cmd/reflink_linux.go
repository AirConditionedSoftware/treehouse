//go:build linux

package cmd

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// cloneFile clones src to dst with the FICLONE ioctl — on Btrfs and XFS a
// metadata-only copy-on-write operation, so multi-gigabyte files land
// near-instantly. dst is created byte-for-byte the way the streamed path
// creates it (O_TRUNC under the same perm), so umask and overwrite
// semantics are identical. If the ioctl then fails (EXDEV across
// filesystems, EOPNOTSUPP on ext4) the truncated dst is fine: any error
// sends the caller to the streamed copy, which rewrites it in full.
func cloneFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if err := unix.IoctlFileClone(int(out.Fd()), int(in.Fd())); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
