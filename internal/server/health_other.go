//go:build !unix

package server

func diskUsage(string) (diskInfo, bool) { return diskInfo{}, false }
