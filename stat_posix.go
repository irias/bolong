// +build !windows,!plan9

package main

import (
	"fmt"
	"os"
	"os/user"
	"syscall"
)

func userGroupName(fi os.FileInfo) (string, string) {
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "u", "g"
	}

	owner := fmt.Sprintf("%d", stat.Uid)
	group := fmt.Sprintf("%d", stat.Gid)
	u, err := user.LookupId(owner)
	if err == nil && u.Username != "" {
		owner = u.Username
	}
	g, err := user.LookupGroupId(group)
	if err == nil && g.Name != "" {
		group = g.Name
	}
	return owner, group
}
