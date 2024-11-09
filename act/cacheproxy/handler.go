package cacheproxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
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

	repositoryName   string
	repositorySecret string
}

func StartHandler(repoName string, targetHost string, outboundIP string, port uint16, cacheSecret string, logger logrus.FieldLogger) (*Handler, error) {
	h := &Handler{}

	if logger == nil {
		discard := logrus.New()
		discard.Out = io.Discard
		logger = discard
	}
	logger = logger.WithField("module", "artifactcache")
	h.logger = logger

	h.repositoryName = repoName
	repoSecret, err := calculateMAC(repoName, cacheSecret)
	if err != nil {
		return nil, fmt.Errorf("unable to decode cacheSecret")
	}
	h.repositorySecret = repoSecret

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
	router.HandlerFunc("GET", urlBase+"/cache", proxyRequestHandler(proxy))
	router.HandlerFunc("POST", urlBase+"/caches", proxyRequestHandler(proxy))
	router.HandlerFunc("PATCH", urlBase+"/caches/:id", proxyRequestHandler(proxy))
	router.HandlerFunc("POST", urlBase+"/caches/:id", proxyRequestHandler(proxy))
	router.HandlerFunc("GET", urlBase+"/artifacts/:id", proxyRequestHandler(proxy))
	router.HandlerFunc("POST", urlBase+"/clean", proxyRequestHandler(proxy))

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
			h.injectAuth(r)
		},
	}
	return proxy, nil
}

func (h *Handler) injectAuth(r *httputil.ProxyRequest) {
	r.Out.SetBasicAuth(h.repositoryName, h.repositorySecret)
}

func (h *Handler) ExternalURL() string {
	// TODO: make the external url configurable if necessary
	return fmt.Sprintf("http://%s:%d",
		h.outboundIP,
		h.listener.Addr().(*net.TCPAddr).Port)
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

func calculateMAC(repoName string, cacheSecret string) (string, error) {
	sec, err := hex.DecodeString(cacheSecret)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, sec)
	mac.Write([]byte(repoName))
	macBytes := mac.Sum(nil)
	return hex.EncodeToString(macBytes), nil
}
