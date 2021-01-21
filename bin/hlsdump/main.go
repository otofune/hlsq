package main

import (
	"context"
	"net/http"
	"os"

	"github.com/otofune/hlsq"
	"github.com/otofune/hlsq/bin/hlsdump/downloader"
	helper "github.com/otofune/hlsq/ctxlogger"
)

func main() {
	ctx := context.TODO()
	logger := helper.NewStdIOLogger()
	ctx = helper.WithLogger(ctx, logger)

	d, err := hlsq.NewMediaPlaylistDownloader(ctx, http.DefaultClient, downloader.SaveRequestWithExponentialBackoffRetry5Times)
	if err != nil {
		panic(err)
	}

	if len(os.Args) != 3 {
		panic("You must specify 2 arguments: url, directory")
	}

	err = d.Download(os.Args[1], os.Args[2])
	if err != nil {
		panic(err)
	}
}
