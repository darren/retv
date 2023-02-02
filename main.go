package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

var addr = flag.String("l", "127.0.0.1:18090", "Listening address")

var errNoRedirect = errors.New("skip redirect")

func noRedirect(req *http.Request, via []*http.Request) error {
	return errNoRedirect
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

func handleHTTP(w http.ResponseWriter, req *http.Request) {
	var err error
	var isSecure bool
	dst := req.URL.String()
	dst = strings.TrimPrefix(dst, "/r/")

	if strings.HasPrefix(dst, "https:") {
		isSecure = true
		dst = strings.TrimPrefix(dst, "https://")
		dst = strings.TrimPrefix(dst, "https:/")
	} else if strings.HasPrefix(dst, "http:") {
		dst = strings.TrimPrefix(dst, "http://")
		dst = strings.TrimPrefix(dst, "http:/")
	}

	dst, err = url.PathUnescape(dst)
	if err != nil {
		log.Print(err)
		return
	}

	if isSecure {
		dst = "https://" + dst
	} else {
		dst = "http://" + dst
	}

	nreq, err := http.NewRequest(req.Method, dst, nil)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
	}

	cloneHeader(nreq.Header, req.Header)

	resp, err := client.Do(nreq)
	if errors.Is(err, errNoRedirect) {
		loc := resp.Header.Get("Location")
		nurl, err := url.Parse(loc)
		if err == nil {
			nloc := fmt.Sprintf("/r/%s/%s?%s", nurl.Host, nurl.Path, nurl.RawQuery)
			log.Printf("[REDIR] %s", nloc)
			resp.Header.Set("Location", nloc)
		} else {
			log.Printf("ParseURL: %v", err)
		}
	} else if err != nil {
		log.Printf("ERROR: %v", err)
		return
	}

	defer resp.Body.Close()
	cloneHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	n, _ := io.Copy(w, resp.Body)

	client := req.Header.Get("X-Forwarded-For")
	if client == "" {
		client, _, _ = net.SplitHostPort(req.RemoteAddr)
	}
	log.Printf("%s [%s] %s %s", client, req.UserAgent(), ByteCount(n), dst)
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
