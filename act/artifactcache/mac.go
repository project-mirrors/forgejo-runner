// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package artifactcache

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"hash"
	"strconv"
	"time"

	"github.com/julienschmidt/httprouter"
)

var (
	ErrValidation   = errors.New("validation error")
	cachePrefixPath = "org:/repo:/run:/ts:/mac:/"
)

func (h *Handler) validateMac(params httprouter.Params) (string, error) {
	ts := params.ByName("ts")

	repo := params.ByName("org") + "/" + params.ByName("repo")
	run := params.ByName("run")
	messageMAC := params.ByName("mac")

	// TODO: allow configurable max age
	if !validateAge(ts) {
		return "", ErrValidation
	}

	expectedMAC := computeMac(h.secret, repo, run, ts).Sum(nil)
	if hmac.Equal([]byte(messageMAC), expectedMAC) {
		return repo, nil
	}
	return repo, ErrValidation
}

func validateAge(ts string) bool {
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if tsInt > time.Now().Unix() {
		return false
	}
	return true
}

func computeMac(key, repo, run, ts string) hash.Hash {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(repo))
	mac.Write([]byte(run))
	mac.Write([]byte(ts))
	return mac
}

func ComputeMac(key, repo, run, ts string) string {
	mac := computeMac(key, repo, run, ts)
	return string(mac.Sum(nil))
}
