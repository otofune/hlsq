package ctxdebugfs

import (
	"context"
	"fmt"
)

type key string

const debugFSKey = key("hlsqDebugFS")

func WithDebugFS(ctx context.Context, fs DebugFS) context.Context {
	return context.WithValue(ctx, debugFSKey, fs)
}

func ExtractDebugFS(ctx context.Context) DebugFS {
	if v := ctx.Value(debugFSKey); v != nil {
		if l, ok := v.(DebugFS); ok {
			return l
		}
		panic(fmt.Errorf("unknown value found in context: %v", v))
	}
	return nil
}
