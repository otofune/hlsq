package ctxlogger

import (
	"context"
	"fmt"
)

type key string

const loggerKey = key("hlsqLoggerKey")

func WithLogger(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

func ExtractLogger(ctx context.Context) Logger {
	if v := ctx.Value(loggerKey); v != nil {
		if l, ok := v.(Logger); ok {
			return l
		}
		panic(fmt.Errorf("unknown value found in context: %v", v))
	}
	return NewDummyLogger()
}
