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
	"time"

	"github.com/grafov/m3u8"
	"github.com/otofune/hlsq/downloader"
	"github.com/otofune/hlsq/helper"
)

const playlistURL = "http://localhost:10000/media/videos/twitch-shortcut/20200418-mu2020-2/1080p60/index-dvr.m3u8"

// PlaylistDownloader
type PlaylistDownloader struct {
	ctx                  context.Context
	df                   func(ctx context.Context, newReq func() (*http.Request, error), dstDirectory string) (err error)
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

func (dl PlaylistDownloader) Download(masterPlaylistURL string, directory string) error {
	logger := helper.ExtractLogger(dl.ctx)
	mediaPlaylist, err := dl.ChoiceBestMediaPlaylist(masterPlaylistURL)
	if err != nil {
		return err
	}
	logger.Debugf("best playlist is %s", mediaPlaylist)

	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}

	mediaPlaylistURL, err := url.Parse(mediaPlaylist)
	if err != nil {
		return err
	}
	for reloadSec, segments := 0, []*m3u8.MediaSegment{}; reloadSec != -1; reloadSec, segments, err = dl.RetriveSegmentByMediaPlaylist(mediaPlaylist) {
		// TODO: セグメント一覧の取得が遅れていないか確認
		// TODO: プレイリストを吐き出す
		if err != nil {
			return err
		}
		for _, seg := range segments {
			u, err := url.Parse(seg.URI)
			tsURL := mediaPlaylistURL.ResolveReference(u).String()
			fileName := fmt.Sprintf("%s.ts", seg.ProgramDateTime.Format("01-02_15:04:05Z07"))
			logger.Debugf(fileName)
			if _, ok := dl.downloadedSegmentURL[tsURL]; ok {
				logger.Debugf("already downloaded: %s", tsURL)
				continue
			}

			dl.downloadedSegmentURL[tsURL] = true
			if err != nil {
				return err
			}
			go func() {
				err := dl.df(
					dl.ctx,
					func() (*http.Request, error) {
						return http.NewRequest("GET", tsURL, nil)
					},
					path.Join(directory, fileName),
				)
				if err != nil {
					logger.Errorf("can not download tsURL: %s", err)
				}
			}()
		}

		if reloadSec != 0 {
			logger.Debugf("waiting for %d sec to reload", reloadSec)
			time.Sleep(time.Duration(reloadSec) * time.Second)
		}
	}

	return nil
}

func (dl PlaylistDownloader) ChoiceBestMediaPlaylist(mayMasterPlaylistURL string) (string, error) {
	logger := helper.ExtractLogger(dl.ctx)

	reader, err := dl.readHTTP(mayMasterPlaylistURL)
	defer reader.Close()

	b, err := ioutil.ReadAll(reader)
	if err != nil {
		return "", err
	}
	logger.Printf("%s\n", b)

	p, _, err := m3u8.Decode(*bytes.NewBuffer(b), true)
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
func (dl PlaylistDownloader) RetriveSegmentByMediaPlaylist(mediaPlaylist string) (reloadDurationInSeconds int, mediaSegments []*m3u8.MediaSegment, err error) {
	logger := helper.ExtractLogger(dl.ctx)

	reader, err := dl.readHTTP(mediaPlaylist)
	if err != nil {
		return -1, nil, err
	}
	defer reader.Close()

	b, err := ioutil.ReadAll(reader)
	if err != nil {
		return -1, nil, err
	}
	logger.Printf("%s\n", b)

	p, _, err := m3u8.Decode(*bytes.NewBuffer(b), true)
	if err != nil {
		return -1, nil, err
	}

	playlist, ok := p.(*m3u8.MediaPlaylist)
	if !ok {
		return -1, nil, fmt.Errorf("not media playlist: %s", mediaPlaylist)
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
