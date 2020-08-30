package http

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/hashicorp/terraform/states/remote"
)

func TestHTTPClient_impl(t *testing.T) {
	var _ remote.Client = new(httpClient)
	var _ remote.ClientLocker = new(httpClient)
}

func TestHTTPClient(t *testing.T) {
	handler := new(testHTTPHandler)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	url, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}

	// Test basic get/update
	client := &httpClient{URL: url, Client: retryablehttp.NewClient()}
	remote.TestClient(t, client)

	// test just a single PUT
	p := &httpClient{
		URL:          url,
		UpdateMethod: "PUT",
		Client:       retryablehttp.NewClient(),
	}
	remote.TestClient(t, p)

	// Test locking and alternative UpdateMethod
	a := &httpClient{
		URL:          url,
		UpdateMethod: "PUT",
		LockURL:      url,
		LockMethod:   "LOCK",
		UnlockURL:    url,
		UnlockMethod: "UNLOCK",
		Client:       retryablehttp.NewClient(),
	}
	b := &httpClient{
		URL:          url,
		UpdateMethod: "PUT",
		LockURL:      url,
		LockMethod:   "LOCK",
		UnlockURL:    url,
		UnlockMethod: "UNLOCK",
		Client:       retryablehttp.NewClient(),
	}
	remote.TestRemoteLocks(t, a, b)

	// test a WebDAV-ish backend
	handler.Reset()
	handler.webDav = true
	client = &httpClient{
		URL:          url,
		UpdateMethod: "PUT",
		Client:       retryablehttp.NewClient(),
	}
	remote.TestClient(t, client) // first time through: 201
	remote.TestClient(t, client) // second time, with identical data: 204

	// test a broken backend
	handler.Reset()
	handler.failNext = true
	remote.TestClient(t, client)
}

type testHTTPHandler struct {
	failNext bool
	webDav bool
	Data   map[string]string
	Locked map[string]bool
}

func (h *testHTTPHandler) Reset() {
	h.failNext = false
	h.webDav = false
	h.Data = nil
	h.Locked = nil
}

func (h *testHTTPHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if h.Data == nil {
		h.Data = map[string]string{}
	}
	if h.Locked == nil {
		h.Locked = map[string]bool{}
	}

	if h.failNext {
		w.WriteHeader(500)
		h.failNext = false
		return
	}
	path := r.URL.Path

	switch r.Method {
	case "GET":
		if d, ok := h.Data[path]; ok {
			w.Write([]byte(d))
		} else {
			w.WriteHeader(404)
		}
	case "PUT":
		buf := new(bytes.Buffer)
		if _, err := io.Copy(buf, r.Body); err != nil {
			w.WriteHeader(500)
		}
		bufAsString := string(buf.Bytes())

		// only difference from webdav function is 204 on match
		if d, ok := h.Data[path]; 
			ok && h.webDav && bufAsString == d {
			w.WriteHeader(204)
		} else {
			w.WriteHeader(201)
		}

		h.Data[path] = bufAsString
	case "POST":
		buf := new(bytes.Buffer)
		if _, err := io.Copy(buf, r.Body); err != nil {
			w.WriteHeader(500)
		}
		h.Data[path] = string(buf.Bytes())
		w.WriteHeader(201)
	case "LOCK":
		if v, ok := h.Locked[path]; ok && v {
			w.WriteHeader(423)
		} else {
			h.Locked[path] = true
		}
	case "UNLOCK":
		delete(h.Locked, path)
	case "DELETE":
		delete(h.Data, path)
		w.WriteHeader(200)
	default:
		w.WriteHeader(500)
		w.Write([]byte(fmt.Sprintf("Unknown method: %s", r.Method)))
	}
}
