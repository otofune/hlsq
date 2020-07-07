package main

import (
	"context"
	"net/http"
	"os"

	"github.com/otofune/hlsq/helper"
	"github.com/otofune/hlsq/logger"
)

func main() {
	ctx := context.TODO()
	logger := logger.NewStdIOLogger()
	ctx = helper.WithLogger(ctx, logger)
	ctx = helper.WithHeader(ctx, http.Header{})

	d, err := NewMediaPlaylistDownloader(ctx)
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
