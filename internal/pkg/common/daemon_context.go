// Copyright 2025 The Forgejo Authors
// SPDX-License-Identifier: MIT

package common

import (
	"context"
)

type daemonContextKey string

const daemonContextKeyVal = daemonContextKey("daemon")

func DaemonContext(ctx context.Context) context.Context {
	val := ctx.Value(daemonContextKeyVal)
	if val != nil {
		if daemon, ok := val.(context.Context); ok {
			return daemon
		}
	}
	return context.Background()
}

func WithDaemonContext(ctx, daemon context.Context) context.Context {
	return context.WithValue(ctx, daemonContextKeyVal, daemon)
}
