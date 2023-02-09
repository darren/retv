package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/pion/rtp"
)

func handleRTP(w http.ResponseWriter, req *http.Request) {
	var path = req.URL.Path
	var wc int64

	if inf == nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "no multicast available")
		return
	}

	parts := strings.FieldsFunc(req.URL.Path, func(r rune) bool { return r == '/' })

	if len(parts) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "No address specified")
		return
	}

	start := time.Now()

	raddr := parts[1]

	addr, err := net.ResolveUDPAddr("udp4", raddr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, err.Error())
		return
	}

	conn, err := net.ListenMulticastUDP("udp4", inf, addr)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, err.Error())
		return
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add((*timeout)))

	var buf = make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, err.Error())
		return
	}
	conn.SetReadDeadline(time.Time{})

	p := &rtp.Packet{}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)

	for {
		if err = p.Unmarshal(buf[:n]); err != nil {
			return
		}

		if _, werr := w.Write(p.Payload); werr != nil {
			break
		} else {
			wc += int64(n)
		}

		if n, err = conn.Read(buf); err != nil {
			break
		}
	}

	if err != nil && err != io.EOF {
		log.Printf("process rtp failed: %v", err)
	}

	client := req.Header.Get("X-Forwarded-For")
	if client == "" {
		client, _, _ = net.SplitHostPort(req.RemoteAddr)
	}

	log.Printf("[RTP] %s %s %s %s [%s]", client, path, time.Since(start), ByteCount(wc), req.UserAgent())
}
