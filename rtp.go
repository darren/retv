package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtp"
)

// https://en.wikipedia.org/wiki/RTP_payload_formats
const (
	RTP_Payload_MP2T = 33
)

const (
	// https://www.w3.org/2013/12/byte-stream-format-registry/mp2t-byte-stream-format.html
	ContentType_MP2T    = "video/MP2T"
	ContentType_DEFAULT = "application/octet-stream"
)

type Event struct {
	etype   EventType
	address string
}

type EventType int

const (
	evtStreamClose = EventType(0)
)

type Handler func(evt *Event) error

type Stream struct {
	sync.RWMutex
	address     string
	contentType string
	conn        *net.UDPConn
	readers     map[string]*Reader
	listeners   []Handler
}

func NewStream(address string, timeout time.Duration) (stream *Stream, err error) {
	var (
		addr        *net.UDPAddr
		conn        *net.UDPConn
		p           *rtp.Packet
		n           int
		buf         []byte
		contentType string
	)

	if addr, err = net.ResolveUDPAddr("udp4", address); err != nil {
		return nil, err
	}

	if conn, err = net.ListenMulticastUDP("udp4", inf, addr); err != nil {
		return nil, err
	}

	buf = make([]byte, 1500)
	conn.SetReadDeadline(time.Now().Add((timeout)))
	if n, err = conn.Read(buf); err != nil {
		return nil, err
	}

	p = &rtp.Packet{}
	if err = p.Unmarshal(buf[:n]); err != nil {
		return nil, err
	}
	if p.PayloadType == RTP_Payload_MP2T {
		contentType = ContentType_MP2T
	} else {
		contentType = ContentType_DEFAULT
	}

	stream = &Stream{
		address:     address,
		conn:        conn,
		contentType: contentType,
		readers:     make(map[string]*Reader),
	}

	go stream.ioLoop()
	return stream, nil
}

func (s *Stream) ioLoop() (err error) {
	defer func() {
		debugLog.Printf("ioLoop exited: %v", err)
		for _, reader := range s.readers {
			close(reader.buf)
		}
		s.Close()
		for _, h := range s.listeners {
			h(&Event{
				address: s.address,
				etype:   evtStreamClose,
			})
		}
	}()

	var (
		p    rtp.Packet
		n    int
		pool bytes.Buffer
		data []byte

		timer = time.NewTicker(time.Second)
		buf   = make([]byte, 1500)
	)

	// reset read deadline
	s.conn.SetReadDeadline(time.Time{})
	for {
		if n, err = s.conn.Read(buf); err != nil {
			return err
		}
		if err = p.Unmarshal(buf[:n]); err != nil {
			return err
		}
		if _, err = pool.Write(p.Payload); err != nil {
			return err
		}

		select {
		case <-timer.C:
			b := pool.Bytes()
			data = make([]byte, len(b))
			copy(data, b)
			if err = s.Broadcast(data); err != nil {
				return err
			}
			pool.Reset()
		default:
			// continue to read next packet
		}
	}
}

func (s *Stream) Broadcast(data []byte) error {
	s.Lock()
	rl := len(s.readers)
	var rs = make([]chan []byte, 0, rl)
	for _, reader := range s.readers {
		rs = append(rs, reader.buf)
	}
	s.Unlock()

	if len(rs) == 0 {
		return fmt.Errorf("no more clients in %s", s.address)
	}

	for _, r := range rs {
		select {
		case r <- data:
			//debugLog.Printf("Send buf....")
		default:
			debugLog.Printf("Send buf skippped")
		}
	}

	return nil
}

func (s *Stream) OnEvent(h Handler) {
	s.listeners = append(s.listeners, h)
}

func (s *Stream) Join(client string) (reader *Reader, err error) {
	debugLog.Printf("%s Join %s", client, s.address)
	reader = &Reader{
		buf: make(chan []byte, 1),
	}
	s.Lock()
	s.readers[client] = reader
	s.Unlock()
	return
}

func (s *Stream) Leave(client string) {
	debugLog.Printf("%s Leave", client)
	s.Lock()
	delete(s.readers, client)
	s.Unlock()
}

func (s *Stream) Close() (err error) {
	return s.conn.Close()
}

type Reader struct {
	buf chan []byte
	e   []byte
}

func (r *Reader) Read(buf []byte) (n int, err error) {
	var (
		data []byte
		ok   bool
	)

	if len(r.e) == 0 {
		data, ok = <-r.buf
		if !ok {
			err = io.EOF
			return
		}
	} else {
		data = r.e
	}

	n = copy(buf, data)
	if len(data) > n {
		r.e = data[n:]
	} else {
		r.e = nil
	}

	return
}

type RTPServer struct {
	sync.RWMutex
	streams map[string]*Stream
}

func NewRTPServer() *RTPServer {
	return &RTPServer{
		streams: make(map[string]*Stream),
	}
}

func (r *RTPServer) Find(address string, timeout time.Duration) (*Stream, error) {
	debugLog.Printf("Find channel %s", address)
	r.RLock()
	stream, ok := r.streams[address]
	r.RUnlock()
	if ok {
		return stream, nil
	}
	stream, err := NewStream(address, timeout)
	if err != nil {
		return nil, err
	}
	r.Lock()
	stream.OnEvent(r.HandleEvent)
	r.streams[address] = stream
	r.Unlock()
	return stream, nil
}

func (r *RTPServer) HandleEvent(evt *Event) error {
	if evt.etype == evtStreamClose {
		debugLog.Printf("Handle Stream Close for: %s", evt.address)
		r.Lock()
		delete(r.streams, evt.address)
		r.Unlock()
	}
	return nil
}

var rtpServer = NewRTPServer()

func handleRTP(w http.ResponseWriter, req *http.Request) {
	var (
		path   = req.URL.Path
		start  = time.Now()
		parts  []string
		err    error
		wc     int64
		stream *Stream
		r      io.Reader
	)

	if inf == nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "no multicast available")
		return
	}

	parts = strings.FieldsFunc(req.URL.Path, func(r rune) bool { return r == '/' })
	if len(parts) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "No address specified")
		return
	}

	if stream, err = rtpServer.Find(parts[1], *timeout); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, err.Error())
		return
	}

	if r, err = stream.Join(req.RemoteAddr); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, err.Error())
		return
	}
	defer stream.Leave(req.RemoteAddr)

	w.Header().Set("Content-Type", stream.contentType)
	w.WriteHeader(http.StatusOK)
	wc, _ = io.Copy(w, r)

	client := req.Header.Get("X-Forwarded-For")
	if client == "" {
		client, _, _ = net.SplitHostPort(req.RemoteAddr)
	}
	log.Printf("[RTP] %s %s %s %s [%s]", client, path, time.Since(start), ByteCount(wc), req.UserAgent())
}
