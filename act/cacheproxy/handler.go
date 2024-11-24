// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cacheproxy

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/sirupsen/logrus"

	"github.com/nektos/act/pkg/common"
)

const (
	urlBase = "/_apis/artifactcache"
)

type Handler struct {
	router   *httprouter.Router
	listener net.Listener
	server   *http.Server
	logger   logrus.FieldLogger

	outboundIP string

	cacheServerHost string

	cacheSecret string

	runs map[string]RunData
}

type RunData struct {
	repositoryFullName string
	runNumber          string
	timestamp          string
	repositoryMAC      string
}

func (h *Handler) CreateRunData(fullName string, runNumber string, timestamp string) RunData {
	mac := computeMac(h.cacheSecret, fullName, runNumber, timestamp)
	return RunData{
		repositoryFullName: fullName,
		runNumber:          runNumber,
		timestamp:          timestamp,
		repositoryMAC:      mac,
	}
}

func StartHandler(targetHost string, outboundIP string, port uint16, cacheSecret string, logger logrus.FieldLogger) (*Handler, error) {
	h := &Handler{}

	if logger == nil {
		discard := logrus.New()
		discard.Out = io.Discard
		logger = discard
	}
	logger = logger.WithField("module", "artifactcache")
	h.logger = logger

	h.cacheSecret = cacheSecret
	h.runs = make(map[string]RunData)

	if outboundIP != "" {
		h.outboundIP = outboundIP
	} else if ip := common.GetOutboundIP(); ip == nil {
		return nil, fmt.Errorf("unable to determine outbound IP address")
	} else {
		h.outboundIP = ip.String()
	}

	h.cacheServerHost = targetHost

	proxy, err := h.newReverseProxy(targetHost)
	if err != nil {
		return nil, fmt.Errorf("unable to set up proxy to target host")
	}

	router := httprouter.New()
	router.HandlerFunc("GET", "/:runId"+urlBase+"/cache", proxyRequestHandler(proxy))
	router.HandlerFunc("POST", "/:runId"+urlBase+"/caches", proxyRequestHandler(proxy))
	router.HandlerFunc("PATCH", "/:runId"+urlBase+"/caches/:id", proxyRequestHandler(proxy))
	router.HandlerFunc("POST", "/:runId"+urlBase+"/caches/:id", proxyRequestHandler(proxy))
	router.HandlerFunc("GET", "/:runId"+urlBase+"/artifacts/:id", proxyRequestHandler(proxy))
	router.HandlerFunc("POST", "/:runId"+urlBase+"/clean", proxyRequestHandler(proxy))

	h.router = router

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port)) // listen on all interfaces
	if err != nil {
		return nil, err
	}
	server := &http.Server{
		ReadHeaderTimeout: 2 * time.Second,
		Handler:           router,
	}
	go func() {
		if err := server.Serve(listener); err != nil && errors.Is(err, net.ErrClosed) {
			logger.Errorf("http serve: %v", err)
		}
	}()
	h.listener = listener
	h.server = server

	return h, nil
}

func proxyRequestHandler(proxy *httputil.ReverseProxy) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	}
}

func (h *Handler) newReverseProxy(targetHost string) (*httputil.ReverseProxy, error) {
	url, err := url.Parse(targetHost)
	if err != nil {
		return nil, err
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(url)
			r.Out.Host = r.In.Host // if desired
			re := regexp.MustCompile(`/(\w+)/_apis/artifactcache`)
			matches := re.FindStringSubmatch(r.In.URL.Path)
			id := matches[1]
			data := h.runs[id]

			r.Out.Header.Add("Forgejo-Cache-Repo", data.repositoryFullName)
			r.Out.Header.Add("Forgejo-Cache-RunNumber", data.runNumber)
			r.Out.Header.Add("Forgejo-Cache-Timestamp", data.timestamp)
			r.Out.Header.Add("Forgejo-Cache-MAC", data.repositoryMAC)
		},
	}
	return proxy, nil
}

func (h *Handler) ExternalURL() string {
	// TODO: make the external url configurable if necessary
	return fmt.Sprintf("http://%s:%d",
		h.outboundIP,
		h.listener.Addr().(*net.TCPAddr).Port)
}

// Informs the proxy of a workflow run that can make cache requests.
// The RunData contains the information about the repository.
// The function returns the 32-bit random key which the run will use to identify itself.
func (h *Handler) AddRun(data RunData) (string, error) {
	keyBytes := make([]byte, 4)
	_, err := rand.Read(keyBytes)
	if err != nil {
		return "", errors.New("Could not generate the run id")
	}
	key := hex.EncodeToString(keyBytes)

	h.runs[key] = data

	return key, nil
}

func (h *Handler) RemoveRun(runID string) error {
	_, exists := h.runs[runID]
	if !exists {
		return errors.New("The run id was not known to the proxy")
	}
	delete(h.runs, runID)
	return nil
}

func (h *Handler) Close() error {
	if h == nil {
		return nil
	}
	var retErr error
	if h.server != nil {
		err := h.server.Close()
		if err != nil {
			retErr = err
		}
		h.server = nil
	}
	if h.listener != nil {
		err := h.listener.Close()
		if errors.Is(err, net.ErrClosed) {
			err = nil
		}
		if err != nil {
			retErr = err
		}
		h.listener = nil
	}
	return retErr
}

func computeMac(secret, repo, run, ts string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(repo))
	mac.Write([]byte(run))
	mac.Write([]byte(ts))
	return hex.EncodeToString(mac.Sum(nil))
}
