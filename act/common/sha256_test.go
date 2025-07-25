// Copyright 2025 The Forgejo Authors
// SPDX-License-Identifier: MIT
package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSha256(t *testing.T) {
	assert.Equal(t, "3fc9b689459d738f8c88a3a48aa9e33542016b7a4052e001aaa536fca74813cb", Sha256("something"))
}
