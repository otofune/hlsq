package hlsq

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/grafov/m3u8"
	"github.com/otofune/hlsq/ctxlogger"
	"golang.org/x/sync/errgroup"
)

// PlayHandler ph
type PlayHandler interface {
	// Receive called in goroutine. order isn't guaranteed, you must sort segments by sequence + discontinuity sequence to persist.
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

			resp, err := DoGetWithBackoffRetry(ctx, hc, mediaPlaylist)
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
						continue // ignore
					}

					// goroutine が実行されるタイミングは不明なので、コピーして値を保持する
					cdseq := dseq
					cseq := seq
					cseg := *seg

					eg.Go(func() error {
						logger.Debugf("goroutine run for id: …%s", id[len(id)-50:])
						return ph.Receive(cctx, &MediaSegment{
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
