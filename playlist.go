package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/grafov/m3u8"
	"github.com/otofune/hlsq/downloader"
	"github.com/otofune/hlsq/helper"
)

const playlistURL = "http://localhost:10000/media/videos/twitch-shortcut/20200418-mu2020-2/1080p60/index-dvr.m3u8"

// PlaylistDownloader
type PlaylistDownloader struct {
	ctx                  context.Context
	df                   func(ctx context.Context, sem chan bool, newReq func() (*http.Request, error), dstDirectory string) (err error)
	downloadedSegmentURL map[string]bool
}

// NewMediaPlaylistDownloader generator
func NewMediaPlaylistDownloader(ctx context.Context) (*PlaylistDownloader, error) {
	return &PlaylistDownloader{
		ctx:                  ctx,
		df:                   downloader.SaveRequestWithExponentialBackoffRetry5Times,
		downloadedSegmentURL: map[string]bool{},
	}, nil
}

func (dl PlaylistDownloader) persistPlaylist(file *os.File, segments []*m3u8.MediaSegment, closed bool) error {
	playlist, err := m3u8.NewMediaPlaylist(0, uint(len(segments)))
	if err != nil {
		return err
	}

	// FIXME: true にしても #EXT-X-ENDLIST が追記されていない気がする
	playlist.Closed = closed

	for _, s := range segments {
		if err := playlist.AppendSegment(s); err != nil {
			return err
		}
	}

	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	if _, err := file.Write(playlist.Encode().Bytes()); err != nil {
		return err
	}

	return nil
}

func (dl PlaylistDownloader) Download(masterPlaylistURL string, directory string) error {
	logger := helper.ExtractLogger(dl.ctx)

	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	playlistDirectory := path.Join(directory, "./playlists")
	if err := os.MkdirAll(playlistDirectory, 0o755); err != nil {
		return err
	}

	// retrive master playlist
	reader, err := dl.readHTTP(masterPlaylistURL)
	if err != nil {
		return err
	}
	defer reader.Close()

	// FIXME: 保持時間が長い
	masterPlaylistBody, err := ioutil.ReadAll(reader)
	if err != nil {
		return err
	}

	masterPlaylistFp, err := os.OpenFile(path.Join(playlistDirectory, "master.m3u8"), os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := masterPlaylistFp.Write(masterPlaylistBody); err != nil {
		return err
	}
	masterPlaylistFp.Close()

	// find media playlist
	mediaPlaylistURLString, err := dl.ChoiceBestMediaPlaylist(masterPlaylistBody, masterPlaylistURL)
	if err != nil {
		return err
	}
	logger.Debugf("best playlist is %s", mediaPlaylistURLString)

	mediaPlaylistURL, err := url.Parse(mediaPlaylistURLString)
	if err != nil {
		return err
	}

	playFp, err := os.OpenFile(path.Join(directory, "play.m3u8"), os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer playFp.Close()

	var downloadWaitGroup sync.WaitGroup
	// TODO: 並列数をコントロールできるようにする
	downloadSemaphore := make(chan bool, 20)
	// ライブラリの AppendSegment が append を使っておらず使いものにならないため自前で管理する
	allSegments := []*m3u8.MediaSegment{}
	for times := 0; true; times++ {
		reader, err := dl.readHTTP(mediaPlaylistURLString)
		defer reader.Close()

		mediaPlaylistBody, err := ioutil.ReadAll(reader)
		if err != nil {
			return err
		}

		reloadSec, segments, err := dl.RetriveSegmentByMediaPlaylist(mediaPlaylistBody)
		if err != nil {
			return err
		}

		// persist
		mediaPlaylistFp, err := os.OpenFile(path.Join(playlistDirectory, fmt.Sprintf("%d.m3u8", times)), os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return err
		}
		if _, err := mediaPlaylistFp.Write(mediaPlaylistBody); err != nil {
			return err
		}
		mediaPlaylistFp.Close()

		for _, seg := range segments {
			u, err := url.Parse(seg.URI)
			tsURL := mediaPlaylistURL.ResolveReference(u).String()
			var fileName string
			if seg.ProgramDateTime.IsZero() {
				fileName = path.Base(u.Path)
			} else {
				fileName = fmt.Sprintf("%s.ts", seg.ProgramDateTime.Format("01-02_15:04:05Z07"))
			}
			if _, ok := dl.downloadedSegmentURL[tsURL]; ok {
				logger.Debugf("already downloaded: %s", tsURL)
				continue
			}

			var copiedSeg m3u8.MediaSegment = *seg
			copiedSeg.URI = fileName
			allSegments = append(allSegments, &copiedSeg)

			dl.downloadedSegmentURL[tsURL] = true
			if err != nil {
				return err
			}
			go func() {
				downloadWaitGroup.Add(1)
				defer downloadWaitGroup.Done()
				err := dl.df(
					dl.ctx,
					downloadSemaphore,
					func() (*http.Request, error) {
						return http.NewRequest("GET", tsURL, nil)
					},
					path.Join(directory, fileName),
				)
				if err != nil {
					logger.Errorf("can not download %s: %s", tsURL, err)
				}
			}()
		}

		logger.Debugf("updating live playlist")
		if err := dl.persistPlaylist(playFp, allSegments, false); err != nil {
			logger.Errorf("saving new playlist failed: %v", err)
		}

		if reloadSec == -1 {
			break
		}

		logger.Debugf("waiting for %d sec to reload", reloadSec)
		time.Sleep(time.Duration(reloadSec) * time.Second)
	}

	downloadWaitGroup.Wait()

	logger.Debugf("saving vod playlist")
	if err := dl.persistPlaylist(playFp, allSegments, true); err != nil {
		return err
	}

	return nil
}

func (dl PlaylistDownloader) ChoiceBestMediaPlaylist(mayMasterPlaylistBody []byte, mayMasterPlaylistURL string) (string, error) {
	logger := helper.ExtractLogger(dl.ctx)

	p, _, err := m3u8.Decode(*bytes.NewBuffer(mayMasterPlaylistBody), true)
	if err != nil {
		return "", err
	}

	switch playlist := p.(type) {
	case *m3u8.MasterPlaylist:
		maxBandwidth := uint32(0)
		bestURL := ""
		for _, v := range playlist.Variants {
			if v != nil {
				logger.Debugf("media playlist found: %v", v)
				if v.Bandwidth > maxBandwidth {
					maxBandwidth = v.Bandwidth
					bestURL = v.URI
				}
			}
		}
		if bestURL == "" {
			return "", fmt.Errorf("something wrong, bestURL is empty")
		}
		return bestURL, nil
	case *m3u8.MediaPlaylist:
		logger.Debugf("media playlist was given as master playlist")
		return mayMasterPlaylistURL, nil
	default:
		return "", fmt.Errorf("something wrong, given m3u8 is not valid playlist")
	}
}

// RetriveSegmentByMediaPlaylist returns -1 as reloadDurationInSeconds when playlist is final
func (dl PlaylistDownloader) RetriveSegmentByMediaPlaylist(masterPlaylistBody []byte) (reloadDurationInSeconds int, mediaSegments []*m3u8.MediaSegment, err error) {
	logger := helper.ExtractLogger(dl.ctx)

	p, _, err := m3u8.Decode(*bytes.NewBuffer(masterPlaylistBody), true)
	if err != nil {
		return -1, nil, err
	}

	playlist, ok := p.(*m3u8.MediaPlaylist)
	if !ok {
		return -1, nil, fmt.Errorf("not media playlist: %s", masterPlaylistBody)
	}

	extinf := 0.0
	for _, s := range playlist.Segments {
		// なぜか nil が入ることがあるので除外する (え？)
		// 1024 件で初期化してるコードがあってそれのせいっぽい
		if s != nil {
			if s.ProgramDateTime.After(time.Now()) {
				logger.Debugf("%s is created in future", s.URI)
				continue
			}
			extinf += s.Duration
			mediaSegments = append(mediaSegments, s)
		}
	}

	if playlist.Closed {
		return -1, mediaSegments, nil
	}
	return int(extinf) / 2, mediaSegments, nil
}

func (dl PlaylistDownloader) readHTTP(url string) (io.ReadCloser, error) {
	headers, err := helper.ExtractHeader(dl.ctx)
	if err != nil {
		return nil, fmt.Errorf("headers doesn't set in context: %w", err)
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header = headers
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}
