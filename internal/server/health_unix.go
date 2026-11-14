//go:build unix

package server

import (
	"os"
	"syscall"
)

// diskUsage reports the filesystem holding path. dev identifies the
// filesystem so paths on the same one are reported once.
func diskUsage(path string) (diskInfo, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return diskInfo{}, false
	}
	var dev uint64
	if fi, err := os.Stat(path); err == nil {
		if sys, ok := fi.Sys().(*syscall.Stat_t); ok {
			dev = uint64(sys.Dev) //nolint:unconvert // Dev is int32 on some platforms
		}
	}
	bs := int64(st.Bsize) //nolint:unconvert
	return diskInfo{dev: dev, total: int64(st.Blocks) * bs, free: int64(st.Bavail) * bs}, true
}
