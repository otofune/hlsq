package ctxdebugfs

import (
	"os"
	"path/filepath"
)

type osDebugFS struct {
	dir string
}

func (odfs *osDebugFS) Open(name string) (DebugFSFile, error) {
	return os.OpenFile(filepath.Join(odfs.dir, name), os.O_WRONLY|os.O_CREATE, 0o644)
}

func NewOSDebugFS(dir string) DebugFS {
	return &osDebugFS{dir: dir}
}
