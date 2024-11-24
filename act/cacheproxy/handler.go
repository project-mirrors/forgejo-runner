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

	workflows map[string]WorkflowData
}

type WorkflowData struct {
	repositoryOwner string
	repositoryName  string
	runNumber       string
	timestamp       string
	repositoryMAC   string
}

func (h *Handler) CreateWorkflowData(owner string, name string, runnumber string, timestamp string) WorkflowData {
	repo := owner + "/" + name
	mac := computeMac(h.cacheSecret, repo, runnumber, timestamp)
	return WorkflowData{
		repositoryOwner: owner,
		repositoryName:  name,
		runNumber:       runnumber,
		timestamp:       timestamp,
		repositoryMAC:   mac,
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
	h.workflows = make(map[string]WorkflowData)

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
	// TODO: re-implement this one
}

func (h *Handler) ExternalURL() string {
	// TODO: make the external url configurable if necessary
	return fmt.Sprintf("http://%s:%d",
		h.outboundIP,
		h.listener.Addr().(*net.TCPAddr).Port)
}

// Informs the proxy of a workflow that can make cache requests.
// The WorkflowData contains the information about the repository.
// The function returns the 32-bit random key which the workflow will use to identify itself.
func (h *Handler) AddWorkflow(data WorkflowData) (string, error) {
	keyBytes := make([]byte, 4)
	_, err := rand.Read(keyBytes)
	if err != nil {
		return "", errors.New("Could not generate the workflow key")
	}
	key := hex.EncodeToString(keyBytes)

	h.workflows[key] = data

	return key, nil
}

func (h *Handler) RemoveWorkflow(workflowKey string) error {
	_, exists := h.workflows[workflowKey]
	if !exists {
		return errors.New("The workflow key was not known to the proxy")
	}
	delete(h.workflows, workflowKey)
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
	return string(mac.Sum(nil))
}
