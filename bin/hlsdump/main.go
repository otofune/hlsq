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

func main() {
	if len(os.Args) != 3 {
		panic("You must specify 2 arguments: url, directory")
	}

	ctx := ctxlogger.WithLogger(context.Background(), ctxlogger.NewStdIOLogger())

	playlist := os.Args[1]
	dest := os.Args[2]

	u, err := url.Parse(playlist)
	if err != nil {
		panic(err)
	}

	if err := os.MkdirAll(dest, 0o755); err != nil {
		panic(err)
	}

	ses, err := hlsq.Play(ctx, http.DefaultClient, u, func(va []*hlsq.MediaPlaylist) []*hlsq.MediaPlaylist {
		return va
	}, handler.New(http.DefaultClient, dest))
	if err != nil {
		panic(err)
	}
	defer ses.Close()

	if err := ses.Wait(); err != nil {
		panic(err)
	}
}
