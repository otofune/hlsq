package main

import (
	"context"
	"net/http"

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
	err = d.Download(
		"http://localhost:8005/master.m3u8",
		"./out5")
	if err != nil {
		panic(err)
	}
}
