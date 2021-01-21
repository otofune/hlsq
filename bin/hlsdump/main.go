package main

import (
	"context"
	"net/http"
	"net/url"
	"os"

	"github.com/otofune/hlsq"
	"github.com/otofune/hlsq/bin/hlsdump/handler"
	"github.com/otofune/hlsq/ctxlogger"
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
		return []*hlsq.MediaPlaylist{}
	}
	return []*hlsq.MediaPlaylist{mp}
}

func main() {
	if len(os.Args) != 3 {
		panic("You must specify 2 arguments: url, directory")
	}

	ctx := ctxlogger.WithLogger(context.Background(), ctxlogger.NewStdIOLogger())

	playlist := os.Args[1]
	dest := os.Args[2]

	playlistURL, err := url.Parse(playlist)
	if err != nil {
		panic(err)
	}

	if err := os.MkdirAll(dest, 0o755); err != nil {
		panic(err)
	}

	h := handler.New(http.DefaultClient, dest)
	defer h.Close()

	ses, err := hlsq.Play(ctx, http.DefaultClient, playlistURL, chooseBestOne, h)
	if err != nil {
		panic(err)
	}
	defer ses.Close()

	if err := ses.Wait(); err != nil {
		panic(err)
	}
}
