package hlsq

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/grafov/m3u8"
	"github.com/otofune/hlsq/ctxdebugfs"
	"github.com/otofune/hlsq/ctxlogger"
	"github.com/otofune/hlsq/repeahttp"
	"golang.org/x/sync/errgroup"
)

type PlayHandler interface {
	// Receive called in goroutine. order isn't guaranteed, you must sort segments by sequence + discontinuity sequence to persist.
	Receive(ctx context.Context, m *MediaSegment) error
}

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

func Play(ctx context.Context, hc *http.Client, playlistURL *url.URL, fmpv FilterMediaPlaylistVariantFn, ph PlayHandler) (PlaySession, error) {
	resp, err := repeahttp.Get(ctx, hc, playlistURL)
	if err != nil {
		return nil, err
	}
	resp.Body = ctxdebugfs.Tee(ctx, resp.Body, "master.m3u8")
	defer resp.Body.Close()

	playlist, err := decodeM3U8(resp.Body)
	if err != nil {
		return nil, err
	}

	mediaPlaylists, err := selectVariants(playlistURL, playlist, fmpv)
	if err != nil {
		return nil, err
	}

	ctxlogger.ExtractLogger(ctx).Debugf("Using variants: %+q\n", mediaPlaylists)

	cctx, cancel := context.WithCancel(ctx)
	eg, cctx := errgroup.WithContext(cctx)
	p := playSession{
		cancel: cancel,
		eg:     eg,
	}

	for _, mp := range mediaPlaylists {
		mpu := *mp
		eg.Go(func() error {
			return runPilot(cctx, hc, &mpu, ph)
		})
	}

	return &p, nil
}

func runPilot(ctx context.Context, hc *http.Client, mediaPlaylist *url.URL, ph PlayHandler) error {
	// handler を goroutine で呼び出すのでそのために使う
	// cctx はあくまで errgroup 配下に渡す、そうしないと子のエラーで意図せず親の処理が止まることになる
	eg, cctx := errgroup.WithContext(ctx)

	logger := ctxlogger.ExtractLogger(ctx)

	seenSegmentSet := sync.Map{}

	waitNextSegment := time.Duration(0)

INFINITE_LOOP:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-cctx.Done():
			// 問題が起きた場合
			return eg.Wait()
		default:
			time.Sleep(waitNextSegment)
			logger.Debugf("fetching media playlist (%s waited)\n", waitNextSegment.String())

			resp, err := repeahttp.Get(ctx, hc, mediaPlaylist)
			if err != nil {
				return err
			}
			if resp.StatusCode > 399 {
				return fmt.Errorf("can not get media playlist, server respond with %d", resp.StatusCode)
			}
			resp.Body = ctxdebugfs.Tee(ctx, resp.Body, fmt.Sprintf("%d.m3u8", time.Now().Unix()))
			pl, err := decodeM3U8(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}

			mp, ok := pl.(*m3u8.MediaPlaylist)
			if !ok {
				return fmt.Errorf("unexpected playlist decoded: master playlist")
			}

			disconSeq := mp.DiscontinuitySeq
			seq := mp.SeqNo
			for _, seg := range mp.Segments {
				if seg == nil {
					continue
				}

				if seg.Discontinuity {
					disconSeq++
					seq = 0
				}
				currentSeq := seq
				seq++

				id := fmt.Sprintf("%s:%d/%d", mediaPlaylist.String(), disconSeq, currentSeq)
				if _, seen := seenSegmentSet.LoadOrStore(id, struct{}{}); seen {
					continue // ignore
				}

				clonedMPURL := *mediaPlaylist
				hmseg := MediaSegment{
					MediaSegment:          *seg,
					Sequence:              currentSeq,
					DiscontinuitySequence: disconSeq,
					Playlist:              &clonedMPURL,
				}

				eg.Go(func() error {
					logger.Debugf("goroutine run for id: …%s", id[len(id)-50:])
					return ph.Receive(cctx, &hmseg)
				})
			}

			if mp.Closed {
				break INFINITE_LOOP
			}
			waitNextSegment = time.Second * time.Duration(mp.TargetDuration)
		}
	}

	return eg.Wait()
}
