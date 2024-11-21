// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package artifactcache

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"hash"

	"github.com/julienschmidt/httprouter"
)

var (
	ErrValidation   = errors.New("repo validation error")
	cachePrefixPath = "repo:/run:/time:/mac:/"
)

func (h *Handler) validateMac(params httprouter.Params) (string, error) {
	repo := params.ByName("repo")
	run := params.ByName("run")
	time := params.ByName("time")
	messageMAC := params.ByName("mac")

	mac := computeMac(h.secret, repo, run, time)
	expectedMAC := mac.Sum(nil)

	if hmac.Equal([]byte(messageMAC), expectedMAC) {
		return repo, nil
	}
	return repo, ErrValidation
}

func computeMac(key, repo, run, time string) hash.Hash {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(repo))
	mac.Write([]byte(run))
	mac.Write([]byte(time))
	return mac
}
