package main

import (
	"fmt"
)

func formatSize(size int64) string {
	if size > 1024*1024*1024 {
		return fmt.Sprintf("%.1fgb", float64(size)/(1024*1024*1024))
	}
	return fmt.Sprintf("%.1fmb", float64(size)/(1024*1024))
}
