package hlsq

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/grafov/m3u8"
	"golang.org/x/sync/errgroup"
)

// MediaSegment ms
type MediaSegment struct {
	m3u8.MediaSegment
	Sequence              uint64
	DiscontinuitySequence uint64
	Playlist              *url.URL
}

// PlayHandler ph
type PlayHandler interface {
	Receive(ctx context.Context, m *MediaSegment) error
}

// PlaySession ps
type PlaySession interface {
	Close() error
	Wait() error
}

type playSession struct {
	cancel context.CancelFunc
	eg     *errgroup.Group
}

func (s *playSession) Close() error {
	s.cancel()
	return s.eg.Wait()
}

func (s *playSession) Wait() error {
	return s.eg.Wait()
}

// Play p
func Play(ctx context.Context, hc *http.Client, playlistURL *url.URL, fmpv FilterMediaPlaylistVariantFn, ph PlayHandler) (PlaySession, error) {
	resp, err := doGet(ctx, hc, playlistURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	playlist, _, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		return nil, err
	}

	mediaPlaylists, err := selectVariants(playlistURL, playlist, fmpv)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	eg, ctx := errgroup.WithContext(ctx)
	p := playSession{
		cancel: cancel,
		eg:     eg,
	}

	for _, mp := range mediaPlaylists {
		fmt.Println(mp)
		mpu := *mp
		eg.Go(func() error {
			return runPilot(ctx, hc, &mpu, ph)
		})
	}

	return &p, nil
}

func runPilot(ctx context.Context, hc *http.Client, mediaPlaylist *url.URL, ph PlayHandler) error {
	// handler を goroutine で呼び出すのでそのために使う
	eg, ctx := errgroup.WithContext(ctx)

	seenSegmentSet := sync.Map{}

	waitNextSegment := time.Duration(0)

INFINITE_LOOP:
	for {
		select {
		case <-ctx.Done():
			// 問題が起きた場合
			return eg.Wait()
		default:
			fmt.Printf("Waiting %s\n", waitNextSegment.String())
			time.Sleep(waitNextSegment)

			resp, err := doGetWithBackoffRetry(ctx, hc, mediaPlaylist)
			if err != nil {
				return err
			}
			if resp.StatusCode > 399 {
				return fmt.Errorf("can not get media playlist, server respond with %d", resp.StatusCode)
			}
			pl, _, err := m3u8.DecodeFrom(resp.Body, true)
			resp.Body.Close()
			if err != nil {
				return err
			}

			mp, ok := pl.(*m3u8.MediaPlaylist)
			if !ok {
				return fmt.Errorf("unexpected playlist decoded: master playlist")
			}

			dseq := mp.DiscontinuitySeq
			seq := mp.SeqNo
			clonedMediaPlaylistURL := *mediaPlaylist
			for _, seg := range mp.Segments {
				if seg != nil {
					if seg.Discontinuity {
						dseq++
						seq = 0
					}

					id := fmt.Sprintf("%s:%d/%d", mediaPlaylist.String(), dseq, seq)
					if _, seen := seenSegmentSet.LoadOrStore(id, struct{}{}); seen {
						continue
					}

					// goroutine が実行されるタイミングは不明なので、コピーして値を保持する
					cdseq := dseq
					cseq := seq
					cseg := *seg

					eg.Go(func() error {
						return ph.Receive(ctx, &MediaSegment{
							MediaSegment:          cseg,
							Sequence:              cseq,
							DiscontinuitySequence: cdseq,
							Playlist:              &clonedMediaPlaylistURL,
						})
					})

					seq++
				}
			}

			if mp.Closed {
				break INFINITE_LOOP
			}
			waitNextSegment = time.Second * time.Duration(mp.TargetDuration)
		}
	}

	return eg.Wait()
}
