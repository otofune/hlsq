package helper

import (
	"context"
	"fmt"
	"net/http"
)

type key string

const headerKey = key("hlsqHeaderKey")

func WithHeader(ctx context.Context, h http.Header) context.Context {
	return context.WithValue(ctx, headerKey, h)
}

func ExtractHeader(ctx context.Context) (http.Header, error) {
	if v := ctx.Value(headerKey); v != nil {
		if h, ok := v.(http.Header); ok {
			return h, nil
		}
		return nil, fmt.Errorf("unknown value found in context: %v", v)
	}
	return nil, fmt.Errorf("context doesn't include http herder")

}

const loggerKey = key("hlsqLoggerKey")

// EmbedLogger represents interface must implemented by embed logger
type EmbedLogger interface {
	Debugf(format string, args ...interface{})
	Printf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
}

func WithLogger(ctx context.Context, l EmbedLogger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

func ExtractLogger(ctx context.Context) EmbedLogger {
	// TODO: 代替のなにもしない logger を指し込む
	if v := ctx.Value(loggerKey); v != nil {
		if l, ok := v.(EmbedLogger); ok {
			return l
		}
		panic(fmt.Errorf("unknown value found in context: %v", v))
	}
	panic(fmt.Errorf("context doesn't include http herder"))
}
