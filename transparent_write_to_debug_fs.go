package hlsq

import (
	"context"
	"io"

	"github.com/otofune/hlsq/ctxdebugfs"
)

type teeReaderCloser struct {
	parent    io.Reader
	mustClose []io.Closer
}

func (r *teeReaderCloser) Close() (err error) {
	for _, rc := range r.mustClose {
		e := rc.Close()
		if e != nil {
			err = e
		}
	}
	return
}

func (r *teeReaderCloser) Read(p []byte) (n int, err error) {
	return r.parent.Read(p)
}

// transparentWriteToDebugFS for debug
func transparentWriteToDebugFS(ctx context.Context, r io.ReadCloser, filename string) io.ReadCloser {
	fs := ctxdebugfs.ExtractDebugFS(ctx)
	if fs == nil {
		return r
	}

	fd, err := fs.Open(filename)
	if err != nil {
		return r
	}

	cr := io.TeeReader(r, fd)

	return &teeReaderCloser{
		mustClose: []io.Closer{r, fd},
		parent:    cr,
	}
}
