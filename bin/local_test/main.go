package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/otofune/hlsq"
)

type handler struct {
}

func (handler) Receive(ctx context.Context, seg *hlsq.MediaSegment) error {
	fmt.Printf("%d/%d\n", seg.DiscontinuitySequence, seg.Sequence)
	return nil
}

func main() {
	ctx := context.TODO()
	u, _ := url.Parse(os.Args[0])
	ses, err := hlsq.Play(ctx, http.DefaultClient, u, func(va []*hlsq.MediaPlaylist) []*hlsq.MediaPlaylist {
		return va
	}, handler{})
	if err != nil {
		panic(err)
	}
	defer ses.Close()

	go func() {
		time.Sleep(5 * time.Second)
		ses.Close()
	}()

	if err := ses.Wait(); err != nil {
		panic(err)
	}
	fmt.Println("waited")
}
