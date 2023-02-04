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
	n, _ := io.Copy(w, resp.Body)

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
