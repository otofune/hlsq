package fshandler

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/otofune/hlsq"
	"github.com/otofune/hlsq/ctxlogger"
	"golang.org/x/sync/errgroup"
)

func New(client *http.Client, dest string) (*FSHandler, error) {
	if err := os.MkdirAll(path.Join(dest, "segments"), 0o755); err != nil {
		return nil, err
	}

	return &FSHandler{
		segmentDirName:    "segments",
		persistentManager: newPersistentManager(client, dest, 8),
	}, nil
}

type FSHandler struct {
	segmentDirName string
	closed         bool

	segs      hlsq.MediaSegments
	segsMutex sync.Mutex

	persistentManager persistentManager
}

var _ hlsq.PlayHandler = &FSHandler{}

func (h *FSHandler) append(seg *hlsq.MediaSegment) {
	h.segsMutex.Lock()
	h.segs = append(h.segs, seg)
	h.segsMutex.Unlock()
}

func (h *FSHandler) deferPersistPlaylistWithoutUpdateInDuration(ctx context.Context, dur time.Duration) error {
	l := len(h.segs)
	// debounce
	time.Sleep(dur)
	if !h.closed && l == len(h.segs) {
		logger := ctxlogger.ExtractLogger(ctx)
		logger.Debugf("saving live play.m3u8")
		// 待ってもアップデートがなければ更新
		return h.persistPlaylist(ctx, false)
	}
	return nil
}

func (h *FSHandler) persistPlaylist(ctx context.Context, closed bool) error {
	h.segsMutex.Lock()
	sorted := h.segs.Sort()
	h.segsMutex.Unlock()

	return h.persistentManager.savePlaylist(ctx, sorted, closed)
}

func (h *FSHandler) Receive(ctx context.Context, seg *hlsq.MediaSegment) error {
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
		return h.persistentManager.saveURLTo(ctx, segmentURI, segmentFilepath)
	})
	if seg.Key != nil {
		eg.Go(func() error {
			return h.persistentManager.saveURLTo(ctx, segKeyURI, keyFilepath)
		})
	}

	return eg.Wait()
}

// Close vod playlist として playlist を保存する
func (h *FSHandler) Close() error {
	h.closed = true
	// TODO なんとかして logger を受けたい
	return h.persistPlaylist(context.Background(), true)
}
