package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var addr = flag.String("l", "127.0.0.1:18090", "Listening address")

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

var bufPool = sync.Pool{
	New: func() any {
		return make([]byte, 8*1024*1024)
	},
}

func handleHTTP(w http.ResponseWriter, req *http.Request) {
	var err error
	var start = time.Now()
	req = req.Clone(req.Context())

	fixURL(req)
	fixReferer(req)

	resp, err := client.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "make request failed:"+err.Error())
		return
	}

	fixLocation(resp)

	defer resp.Body.Close()
	cloneHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	buf := bufPool.Get().([]byte)
	n, _ := io.CopyBuffer(w, resp.Body, buf)
	defer bufPool.Put(buf)

	client := req.Header.Get("X-Forwarded-For")
	if client == "" {
		client, _, _ = net.SplitHostPort(req.RemoteAddr)
	}
	log.Printf("%s %s %v [%s]\n%s\n%s\n------------------------------",
		client, ByteCount(n), time.Since(start),
		req.UserAgent(),
		req.URL.String(),
		req.Referer(),
	)
}

func ByteCount(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}

func main() {
	if os.Getppid() == 1 {
		log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))
	} else {
		log.SetFlags(log.Lshortfile | log.LstdFlags)
	}

	flag.Parse()
	var mux http.ServeMux
	mux.HandleFunc("/r/", handleHTTP)

	log.Fatal(http.ListenAndServe(*addr, &mux))
}
