package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math"
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

func (dl PlaylistDownloader) Download(masterPlaylistURL string, directory string) error {
	logger := helper.ExtractLogger(dl.ctx)

	// FIXME: なぜか append 使ってないライブラリで終わってる
	// .Segments の cap がこれ依存なので適当にデカくしときゃええやろ〜って決めてる
	// あとから cap 拡張はできないので、これオーバーしたら壊れる
	// 0.5 G くらい確保しとけばいいだろ という説 (2^32-1 が 4GB だよねって話されてそれ〜〜ってなった)
	// TODO: Playlist さわれるまともなライブラリを作る
	newPlaylist, err := m3u8.NewMediaPlaylist(0, uint(math.Pow(2, 29)))
	if err != nil {
		return err
	}

	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	playlistDirectory := path.Join(directory, "./playlists")
	if err := os.MkdirAll(playlistDirectory, 0o755); err != nil {
		return err
	}

	reader, err := dl.readHTTP(masterPlaylistURL)
	if err != nil {
		return err
	}
	defer reader.Close()

	masterPlaylistBody, err := ioutil.ReadAll(reader)
	if err != nil {
		return err
	}

	mediaPlaylistURLString, err := dl.ChoiceBestMediaPlaylist(masterPlaylistBody, masterPlaylistURL)
	if err != nil {
		return err
	}
	logger.Debugf("best playlist is %s", mediaPlaylistURLString)

	masterPlaylistFp, err := os.OpenFile(path.Join(playlistDirectory, "master.m3u8"), os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := masterPlaylistFp.Write(masterPlaylistBody); err != nil {
		return err
	}
	masterPlaylistFp.Close()

	// persist
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
	downloadSemaphore := make(chan bool, 20)
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
			if err := newPlaylist.AppendSegment(&copiedSeg); err != nil {
				return err
			}

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
		playFp.Seek(0, 0)
		if _, err := playFp.Write(newPlaylist.Encode().Bytes()); err != nil {
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
	playFp.Seek(0, 0)
	// FIXME: なんか #EXT-X-ENDLIST が追加されてなくて最悪
	newPlaylist.Closed = true
	playFp.Seek(0, 0)
	if _, err := playFp.Write(newPlaylist.Encode().Bytes()); err != nil {
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
