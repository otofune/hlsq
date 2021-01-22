package handler

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/otofune/hlsq"
	"github.com/otofune/hlsq/ctxlogger"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

func New(client *http.Client, dest string) *HLSDumpHandler {
	return &HLSDumpHandler{
		client:     client,
		destDir:    dest,
		downloadSW: semaphore.NewWeighted(8),
	}
}

type HLSDumpHandler struct {
	client         *http.Client
	destDir        string
	segmentDirName string

	segmentMutex sync.Mutex
	segs         hlsq.MediaSegments

	downloadSW *semaphore.Weighted
	downloaded sync.Map
}

func (h *HLSDumpHandler) append(seg *hlsq.MediaSegment) {
	h.segmentMutex.Lock()
	h.segs = append(h.segs, seg)
	h.segmentMutex.Unlock()
}

func (h *HLSDumpHandler) deferPersistPlaylistWithoutUpdateInDuration(ctx context.Context, dur time.Duration) error {
	l := len(h.segs)
	// debounce
	time.Sleep(dur)
	if l == len(h.segs) {
		logger := ctxlogger.ExtractLogger(ctx)
		logger.Debugf("saving live play.m3u8")
		// 待ってもアップデートがなければ更新
		return h.persistPlaylist(false)
	}
	return nil
}

func (h *HLSDumpHandler) persistPlaylist(closed bool) error {
	sorted := h.segs.Sort()
	f, err := os.OpenFile(filepath.Join(h.destDir, "play.m3u8"), os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(sorted.String(closed)); err != nil {
		return err
	}
	return nil
}

func (h *HLSDumpHandler) saveURLTo(ctx context.Context, u *url.URL, path string) error {
	logger := ctxlogger.ExtractLogger(ctx)

	if _, loaded := h.downloaded.LoadOrStore(u.String(), struct{}{}); loaded {
		logger.Debugf("skip %s cuz already got\n", path)
		return nil // skip already got
	}

	logger.Debugf("waiting lock for saving %s\n", path)

	if err := h.downloadSW.Acquire(ctx, 1); err != nil {
		return err
	}
	defer h.downloadSW.Release(1)

	logger.Debugf("acquired lock for saving %s\n", path)

	f, err := os.OpenFile(filepath.Join(h.destDir, path), os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	resp, err := hlsq.DoGetWithBackoffRetry(ctx, h.client, u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}

	logger.Debugf("saved %s\n", path)

	return nil
}

func (h *HLSDumpHandler) Receive(ctx context.Context, seg *hlsq.MediaSegment) error {
	segmentURI, err := url.Parse(seg.URI)
	if err != nil {
		return err
	}
	segmentURI = seg.Playlist.ResolveReference(segmentURI)

	segmentFilepath := path.Join(h.segmentDirName, fmt.Sprintf("%d_%d%s", seg.DiscontinuitySequence, seg.Sequence, path.Ext(segmentURI.Path)))
	seg.URI = segmentFilepath
	keyFilepath := ""

	var segKeyURI *url.URL
	if seg.Key != nil {
		segKeyURI, err = url.Parse(seg.Key.URI)
		if err != nil {
			return err
		}
		segKeyURI = seg.Playlist.ResolveReference(segKeyURI)
		keyFilepath = path.Join(h.segmentDirName, fmt.Sprintf("%x%s", sha1.Sum([]byte(segKeyURI.String())), path.Ext(segKeyURI.Path)))
		seg.Key.URI = keyFilepath
	}

	h.append(seg)

	eg, ctx := errgroup.WithContext(ctx)
	// プレイリストの遅延永続化
	eg.Go(func() error {
		return h.deferPersistPlaylistWithoutUpdateInDuration(ctx, time.Second*time.Duration(seg.Duration/2))
	})
	// ファイルの保存
	eg.Go(func() error {
		return h.saveURLTo(ctx, segmentURI, segmentFilepath)
	})
	if seg.Key != nil {
		eg.Go(func() error {
			return h.saveURLTo(ctx, segKeyURI, keyFilepath)
		})
	}

	return eg.Wait()
}

// Close vod playlist として playlist を保存する
func (h *HLSDumpHandler) Close() error {
	return h.persistPlaylist(true)
}
