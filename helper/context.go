package helper

import (
	"context"
	"fmt"

	"github.com/otofune/hlsq/logger"
)

type key string

const loggerKey = key("hlsqLoggerKey")

func WithLogger(ctx context.Context, l logger.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

func ExtractLogger(ctx context.Context) logger.Logger {
	if v := ctx.Value(loggerKey); v != nil {
		if l, ok := v.(logger.Logger); ok {
			return l
		}
		panic(fmt.Errorf("unknown value found in context: %v", v))
	}
	return logger.NewDummyLogger()
}
