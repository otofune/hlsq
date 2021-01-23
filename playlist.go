package hlsq

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"strings"

	"github.com/grafov/m3u8"
	"golang.org/x/xerrors"
)

type MediaPlaylist struct {
	m3u8.Variant
}

type FilterMediaPlaylistVariantFn func(va []*MediaPlaylist) []*MediaPlaylist

func selectVariants(playlistURL *url.URL, playlist m3u8.Playlist, filter FilterMediaPlaylistVariantFn) ([]*url.URL, error) {
	switch playlist := playlist.(type) {
	case *m3u8.MasterPlaylist:
		var mps []*MediaPlaylist
		for _, v := range playlist.Variants {
			if v != nil {
				v := *v
				mps = append(mps, &MediaPlaylist{v})
				continue
			}
			fmt.Println("nil variant found")
		}
		selected := filter(mps)
		if len(selected) == 0 {
			return nil, xerrors.Errorf("no variants selected")
		}

		var urls []*url.URL
		for _, v := range selected {
			u, err := url.Parse(v.URI)
			if err != nil {
				return nil, xerrors.Errorf("%w", err)
			}
			// resolve relative URL
			u = playlistURL.ResolveReference(u)
			urls = append(urls, u)
		}

		return urls, nil
	case *m3u8.MediaPlaylist:
		return []*url.URL{playlistURL}, nil
	default:
		return nil, xerrors.Errorf("something wrong, given m3u8 is not valid playlist")
	}
}

func decodeM3U8(reader io.Reader) (m3u8.Playlist, error) {
	playlistBody, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, xerrors.Errorf("%w", err)
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
