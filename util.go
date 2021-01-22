package hlsq

import (
	"context"
	"net/http"
	"net/url"
)

func ctxGet(ctx context.Context, hc *http.Client, u *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	return hc.Do(req)
}
