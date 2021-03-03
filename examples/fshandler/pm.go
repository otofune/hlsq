package fshandler

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/otofune/hlsq"
	"github.com/otofune/hlsq/ctxlogger"
	"github.com/otofune/hlsq/repeahttp"
	"golang.org/x/sync/semaphore"
)

type persistentManager struct {
	client  *http.Client
	destDir string

	downloadSW *semaphore.Weighted
	downloaded sync.Map

	playlistMutex sync.Mutex
}

func newPersistentManager(client *http.Client, destDir string, parallelism int64) persistentManager {
	return persistentManager{
		client:     client,
		destDir:    destDir,
		downloadSW: semaphore.NewWeighted(parallelism),
	}
}

func (pm *persistentManager) saveURLTo(ctx context.Context, u *url.URL, path string) error {
	logger := ctxlogger.ExtractLogger(ctx)

	if _, loaded := pm.downloaded.LoadOrStore(u.String(), struct{}{}); loaded {
		logger.Debugf("skip %s cuz already got\n", path)
		return nil // skip already got
	}

	logger.Debugf("waiting lock for saving %s\n", path)

	if err := pm.downloadSW.Acquire(ctx, 1); err != nil {
		return err
	}
	defer pm.downloadSW.Release(1)

	logger.Debugf("acquired lock for saving %s\n", path)

	f, err := os.OpenFile(filepath.Join(pm.destDir, path), os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	resp, err := repeahttp.Get(ctx, pm.client, u)
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

func (pm *persistentManager) savePlaylist(ctx context.Context, playlist hlsq.MediaSegments, playlistClosed bool) error {
	logger := ctxlogger.ExtractLogger(ctx)

	pm.playlistMutex.Lock()
	logger.Debugf("Saving play.m3u8 with lock")
	f, err := os.OpenFile(filepath.Join(pm.destDir, "play.m3u8"), os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(playlist.String(playlistClosed)); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	logger.Debugf("Saved play.m3u8")
	pm.playlistMutex.Unlock()

	return nil
}
