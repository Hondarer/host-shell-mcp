package tools

import "golang.org/x/sys/windows"

// availableBytes returns the space available to this user (honoring quotas) on the volume containing path.
func availableBytes(path string) (uint64, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	var avail uint64
	if windows.GetDiskFreeSpaceEx(p, &avail, nil, nil) != nil {
		return 0, false
	}
	return avail, true
}
