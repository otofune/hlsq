package hlsq

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

func doGet(ctx context.Context, hc *http.Client, u *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	return hc.Do(req)
}

func DoGetWithBackoffRetry(ctx context.Context, hc *http.Client, u *url.URL) (resp *http.Response, err error) {
	for i := time.Duration(0); i < 5; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		sec := (1 << i) >> 1 * time.Second
		time.Sleep(sec)

		if resp != nil {
			resp.Body.Close()
		}

		resp, err = doGet(ctx, hc, u)
		if err != nil {
			continue
		}
		if resp.StatusCode > 399 {
			continue
		}
		break
	}
	return
}
