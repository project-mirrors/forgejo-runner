// Copyright 2025 The Forgejo Authors
// SPDX-License-Identifier: MIT
package common

import (
	"crypto/rand"
	"encoding/hex"
)

func RandName(size int) (string, error) {
	randBytes := make([]byte, size)
	if _, err := rand.Read(randBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randBytes), nil
}
