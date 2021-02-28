package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/otofune/hlsq"
	"github.com/otofune/hlsq/ctxdebugfs"
	"github.com/otofune/hlsq/ctxlogger"
	"github.com/otofune/hlsq/examples/fshandler"
)

func chooseBestOne(va []*hlsq.MediaPlaylist) []*hlsq.MediaPlaylist {
	var mp *hlsq.MediaPlaylist
	maxBandwidth := uint32(0)
	for _, v := range va {
		if v != nil {
			if v.Bandwidth > maxBandwidth {
				maxBandwidth = v.Bandwidth
				v := v // copy
				mp = v
			}
		}
	}
	if mp == nil {
		panic("no playlist available")
	}
	return []*hlsq.MediaPlaylist{mp}
}

func do(playlist *url.URL, dest string) error {
	ctx := ctxlogger.WithLogger(context.Background(), ctxlogger.NewStdIOLogger())

	debugDest := filepath.Join(dest, "debug")
	ctx = ctxdebugfs.WithDebugFS(ctx, ctxdebugfs.NewOSDebugFS(debugDest))
	if err := os.MkdirAll(debugDest, 0o755); err != nil {
		return err
	}

	h, err := fshandler.New(http.DefaultClient, dest)
	if err != nil {
		return err
	}
	defer h.Close()

	ses, err := hlsq.Play(ctx, http.DefaultClient, playlist, chooseBestOne, h)
	if err != nil {
		return err
	}
	defer ses.Close()

	if err := ses.Wait(); err != nil {
		return err
	}
	return nil
}

func main() {
	fmt.Printf("hlsdump %s\n\n", version())

	if len(os.Args) != 3 {
		panic("You must specify 2 arguments: url, directory")
	}

	playlist := os.Args[1]
	dest := os.Args[2]

	playlistURL, err := url.Parse(playlist)
	if err != nil {
		panic(err)
	}

	if err := do(playlistURL, dest); err != nil {
		panic(err)
	}
}
