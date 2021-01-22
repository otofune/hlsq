package repeahttp

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

func ctxGet(ctx context.Context, hc *http.Client, u *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	return hc.Do(req)
}

const retryTimes = 5

func Get(ctx context.Context, hc *http.Client, u *url.URL) (resp *http.Response, err error) {
	for i := time.Duration(0); i < retryTimes; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if resp != nil {
			resp.Body.Close()
		}

		sec := ((1 << i) >> 1) * time.Second
		time.Sleep(sec)

		cctx, cancel := context.WithTimeout(ctx, time.Second*30)
		defer cancel()

		resp, err = ctxGet(cctx, hc, u)
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
