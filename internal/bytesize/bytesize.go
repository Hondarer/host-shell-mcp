// Package bytesize formats byte counts for people, shared by the target's tools and the bridge.
package bytesize

import "fmt"

// Format renders n rounded to one decimal in IEC units (for example "1.5 GiB"); counts below
// 1 KiB are exact ("512 B").
func Format(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
