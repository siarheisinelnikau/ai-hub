//go:build !windows

package status

// win32DiskUsage is an off-Windows stub; DiskUsage uses df there.
func win32DiskUsage(root string) (int64, int64, int64) { return 0, 0, 0 }
