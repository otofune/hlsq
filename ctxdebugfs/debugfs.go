package ctxdebugfs

import (
	"io"
)

type DebugFSFile interface {
	io.WriteCloser
	io.Seeker
}

type DebugFS interface {
	// Open writable DebugFSFile
	Open(name string) (DebugFSFile, error)
}
