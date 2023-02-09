package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
	addr    = flag.String("l", "127.0.0.1:18090", "Listening address")
	iface   = flag.String("i", "eth0", "Listening multicast interface")
	timeout = flag.Duration("o", time.Second, "rtp read timeout")
	bufsize = flag.Int("b", 1, "buffer size in mega bytes")
)

var inf *net.Interface

var bufPool = sync.Pool{
	New: func() any {
		return make([]byte, *bufsize*1024*1024)
	},
}

func main() {
	if os.Getppid() == 1 {
		log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))
	} else {
		log.SetFlags(log.Lshortfile | log.LstdFlags)
	}

	flag.Parse()

	var err error
	inf, err = net.InterfaceByName(*iface)
	if err != nil {
		log.Printf("multicast disabled: %v", err)
	}

	var mux http.ServeMux
	mux.HandleFunc("/r/", handleHLS)
	mux.HandleFunc("/rtp/", handleRTP)

	log.Fatal(http.ListenAndServe(*addr, &mux))
}
