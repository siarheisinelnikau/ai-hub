//go:build windows

package status

import (
	"syscall"
	"unsafe"
)

// win32DiskUsage returns (total, used, free) in bytes for root via
// kernel32!GetDiskFreeSpaceExW — the Windows stand-in for app.py's
// shutil.disk_usage fallback. Returns 0,0,0 if the call fails.
func win32DiskUsage(root string) (total, used, free int64) {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

	var freeAvailPerAlloc, totalSpace, freeSpace uint64
	r1, _, _ := proc.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(root))),
		uintptr(unsafe.Pointer(&freeAvailPerAlloc)),
		uintptr(unsafe.Pointer(&totalSpace)),
		uintptr(unsafe.Pointer(&freeSpace)),
	)
	if r1 == 0 {
		return 0, 0, 0
	}
	return int64(totalSpace), int64(totalSpace - freeSpace), int64(freeSpace)
}
