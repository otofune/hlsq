package hlsq

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/grafov/m3u8"
	"github.com/otofune/hlsq/ctxdebugfs"
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

			resp, err := DoGetWithBackoffRetry(ctx, hc, mediaPlaylist)
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

func decodeM3U8(reader io.Reader) (m3u8.Playlist, error) {
	playlistBody, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	// panic 対策
	// m3u8.Decode は blank line があったときに media segment だと誤認する
	// RFC8216 "Blank lines are ignored." だが、そうなっていない
	// このとき media segment を構造体にする条件として #EXTINF が表われている必要があるコードになっている
	// 空行の場合その条件は満たされないことがある。#EXTINF は直後の MediaSegment にのみ反映されているコードになっているためである
	// さらに EXT-X-KEY などの全体に適用する設定がある場合、メタデータを適用する挙動がある
	// 当然前提として MediaSegment が初期化されている前提があり、上記のようにすり抜けると panic する
	// というわけなので、いったん空行を消して回避する
	playlistString := strings.Join(strings.Split(string(playlistBody), "\n\n"), "\n")

	pl, _, err := m3u8.Decode(*bytes.NewBufferString(playlistString), false)
	return pl, err
}
