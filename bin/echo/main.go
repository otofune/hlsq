package main

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/otofune/hlsq"
	"github.com/otofune/hlsq/ctxlogger"
)

type handler struct {
}

func (handler) Receive(ctx context.Context, seg *hlsq.MediaSegment) error {
	hash := sha1.Sum([]byte(seg.Playlist.String()))
	fmt.Printf("%x:%d/%d\n", hash, seg.DiscontinuitySequence, seg.Sequence)
	return nil
}

func main() {
	ctx := ctxlogger.WithLogger(context.Background(), ctxlogger.NewStdIOLogger())

	u, _ := url.Parse(os.Args[1])
	ses, err := hlsq.Play(ctx, http.DefaultClient, u, func(va []*hlsq.MediaPlaylist) []*hlsq.MediaPlaylist {
		return va
	}, handler{})
	if err != nil {
		panic(err)
	}
	defer ses.Close()

	go func() {
		time.Sleep(1 * time.Minute)
		ses.Close()
	}()

	if err := ses.Wait(); err != nil {
		panic(err)
	}
	fmt.Println("waited")
}
