package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/otofune/hlsq/helper"
)

// SaveRequestWithExponentialBackoffRetry5Times SSIA
// TODO: explain when error will be happen
func SaveRequestWithExponentialBackoffRetry5Times(ctx context.Context, newReq func() (*http.Request, error), dstFile string) error {
	logger := helper.ExtractLogger(ctx)

	var outOfScopeError error

	for i := time.Duration(0); i < 5; i++ {
		outOfScopeError = nil

		select {
		case <-ctx.Done():
			break
		default:
		}

		sec := (1 << i) >> 1 * time.Second
		if sec > 0 {
			logger.Debugf("waiting %d seconds...\n", sec/time.Second)
			time.Sleep(sec)
		}

		req, err := newReq()
		if err != nil {
			return err
		}

		res, err := http.DefaultClient.Do(req)

		if err != nil {
			logger.Errorf("%v", err)
			outOfScopeError = err
			continue
		}

		if res.StatusCode > 399 {
			outOfScopeError = fmt.Errorf("failed to fetch: server returns %d", res.StatusCode)
			continue
		}

		defer res.Body.Close()

		fp, err := os.OpenFile(dstFile, os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return err
		}
		defer fp.Close()

		written, err := io.Copy(fp, res.Body)
		if err != nil {
			logger.Errorf("%v", err)
			outOfScopeError = err
			continue
		}

		if res.ContentLength != -1 && written != res.ContentLength {
			logger.Errorf("%v", err)
			outOfScopeError = err
			continue
		}

		break
	}

	return outOfScopeError
}
