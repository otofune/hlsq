package hlsq

import (
	"net/url"
	"sort"

	"github.com/grafov/m3u8"
)

// MediaSegment represents media segment in HLS stream
type MediaSegment struct {
	m3u8.MediaSegment
	Sequence              uint64
	DiscontinuitySequence uint64
	Playlist              *url.URL
}

type MediaSegments []*MediaSegment

// Sort non-destructive sort
func (mss MediaSegments) Sort() MediaSegments {
	target := make(MediaSegments, len(mss))
	copy(target, mss)

	sort.Slice(target, func(i, j int) bool {
		if target[i].DiscontinuitySequence < target[j].DiscontinuitySequence {
			return true
		}
		if target[i].Sequence < target[j].Sequence {
			return true
		}
		return false
	})
	return target
}

func (mss MediaSegments) String(closed bool) string {
	if mss == nil {
		panic("")
	}
	p, err := m3u8.NewMediaPlaylist(uint(len(mss)), uint(len(mss)))
	if err != nil {
		panic(err)
	}
	p.Closed = closed
	for _, seg := range mss {
		mseg := seg.MediaSegment
		p.AppendSegment(&mseg)
	}
	return p.Encode().String()
}
