// +build windows,plan9

package main

import (
	"os"
)

func userGroupName(fi os.FileInfo) (string, string) {
	return "u", "g"
}
