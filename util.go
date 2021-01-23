package hlsq

import (
	"context"
	"net/http"
	"net/url"

	"golang.org/x/xerrors"
)

func ctxGet(ctx context.Context, hc *http.Client, u *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, xerrors.Errorf("%w", err)
	}
	return hc.Do(req)
}
