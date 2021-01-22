package main

import "runtime/debug"

// version https://blog.lufia.org/entry/2020/12/18/002238
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}
	return info.Main.Version
}
