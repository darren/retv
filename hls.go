package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const m3uMagic = "#EXTM3U"

func noRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

var client = http.Client{
	CheckRedirect: noRedirect,
}

func cloneHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// Copied from https://github.com/golang/go/blob/master/src/net/http/httputil/reverseproxy.go
// Hop-by-hop headers. These are removed when sent to the backend.
// http://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html
var hopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te", // canonicalized version of "TE"
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// removeConnectionHeaders removes hop-by-hop headers listed in the "Connection" header of h.
// See RFC 7230, section 6.1
func removeConnectionHeaders(h http.Header) {
	if c := h.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				h.Del(f)
			}
		}
	}
}

func removeHopHeaders(h http.Header) {
	for _, k := range hopHeaders {
		hv := h.Get(k)
		if hv == "" {
			continue
		}
		if k == "Te" && hv == "trailers" {
			continue
		}
		h.Del(k)
	}
}

// prune clean http header
func prune(h http.Header) {
	removeConnectionHeaders(h)
	removeHopHeaders(h)
}

func normalizeURL(src string) string {
	var (
		isSecure bool
		err      error
	)

	if strings.HasPrefix(src, "https:") {
		isSecure = true
		src = strings.TrimPrefix(src, "https://")
		src = strings.TrimPrefix(src, "https:/")
	} else if strings.HasPrefix(src, "http:") {
		src = strings.TrimPrefix(src, "http://")
		src = strings.TrimPrefix(src, "http:/")
	}

	src, err = url.PathUnescape(src)
	if err != nil {
		log.Printf("normalize url failed: %v", err)
		return src
	}

	if isSecure {
		src = "https://" + src
	} else {
		src = "http://" + src
	}

	return src
}

func fixURL(req *http.Request) {
	prune(req.Header)
	dst := req.URL.String()
	dst = normalizeURL(strings.TrimPrefix(dst, "/r/"))
	nurl, err := url.Parse(dst)
	if err == nil {
		req.URL = nurl
		req.Host = nurl.Host
	} else {
		log.Printf("fix url failed: %v", err)
	}
	req.RequestURI = ""
}

func fixReferer(req *http.Request) {
	referer := req.Referer()
	if referer != "" {
		uref, err := url.Parse(referer)
		if err == nil {
			if strings.HasPrefix(uref.Path, "/r/") {
				nref := strings.TrimPrefix(uref.Path, "/r/")
				if uref.RawQuery != "" {
					nref += "?" + uref.RawQuery
				}
				nref = normalizeURL(nref)
				req.Header.Set("Referer", nref)
			}
		}
	}
}

func fixLocation(resp *http.Response) {
	loc := resp.Header.Get("Location")
	if loc != "" {
		nurl, err := url.Parse(loc)
		if err == nil {
			nloc := fmt.Sprintf("/r/%s%s", nurl.Host, nurl.Path)
			if nurl.RawQuery != "" {
				nloc += "?" + nurl.RawQuery
			}
			resp.Header.Set("Location", nloc)
		} else {
			log.Printf("ParseURL: %v", err)
		}
	}
}

func fixBody(src io.Reader) string {
	var w bytes.Buffer
	scanner := bufio.NewScanner(src)
	for scanner.Scan() {
		text := scanner.Text()
		if strings.HasPrefix(text, "http://") ||
			strings.HasPrefix(text, "https://") {
			fmt.Fprintln(&w, "/r/"+text)
		} else {
			fmt.Fprintln(&w, text)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("fix body failed: %v", err)
	}
	return w.String()
}

var hlsClient int64

func handleHLS(w http.ResponseWriter, req *http.Request) {
	var err error
	var start = time.Now()
	var path = req.URL.Path

	req = req.Clone(req.Context())

	fixURL(req)
	fixReferer(req)
	req.Header.Del("Accept-Encoding") // let net/http handle gzip

	resp, err := client.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "make request failed:"+err.Error())
		return
	}

	fixLocation(resp)

	defer resp.Body.Close()

	var body io.Reader = resp.Body
	if resp.ContentLength == -1 || int(resp.ContentLength) > len(m3uMagic) {
		var peek = make([]byte, len(m3uMagic))
		pn, err := resp.Body.Read(peek)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, "detect magic header failed:"+err.Error())
		}
		body = io.MultiReader(bytes.NewReader(peek[:pn]), resp.Body)
		if bytes.Equal(peek[:pn], []byte(m3uMagic)) {
			fixedBody := fixBody(body)
			resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(fixedBody)))
			body = strings.NewReader(fixedBody)
		}
	}

	atomic.AddInt64(&hlsClient, 1)
	updateStatus()
	defer updateStatus()
	defer atomic.AddInt64(&hlsClient, -1)

	cloneHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	n, _ := io.Copy(w, body)

	client := req.Header.Get("X-Forwarded-For")
	if client == "" {
		client, _, _ = net.SplitHostPort(req.RemoteAddr)
	}
	log.Printf("[HLS] %s %s %s %s [%s]", client, path, time.Since(start), ByteCount(n), req.UserAgent())
}
