// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os/signal"
	"syscall"

	"code.forgejo.org/forgejo/runner/v11/internal/app/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// run the command
	cmd.Execute(ctx)
}
