package hlsq

import (
	"fmt"
	"net/url"

	"github.com/grafov/m3u8"
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
			return nil, fmt.Errorf("no variants selected")
		}

		var urls []*url.URL
		for _, v := range selected {
			u, err := url.Parse(v.URI)
			if err != nil {
				// TODO: wrap
				return nil, err
			}
			// resolve relative URL
			u = playlistURL.ResolveReference(u)
			urls = append(urls, u)
		}

		return urls, nil
	case *m3u8.MediaPlaylist:
		return []*url.URL{playlistURL}, nil
	default:
		return nil, fmt.Errorf("something wrong, given m3u8 is not valid playlist")
	}

}
