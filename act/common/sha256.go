// Copyright 2025 The Forgejo Authors
// SPDX-License-Identifier: MIT
package common

import (
	"crypto/sha256"
	"encoding/hex"
)

func Sha256(content string) string {
	hashBytes := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hashBytes[:])
}
