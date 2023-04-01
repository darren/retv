package main

import (
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"
)

var (
	addr    = flag.String("l", "127.0.0.1:18090", "Listening address")
	iface   = flag.String("i", "eth0", "Listening multicast interface")
	timeout = flag.Duration("o", time.Second, "rtp read timeout")
	debug   = flag.Bool("d", false, "enable debug log")
)

var inf *net.Interface
var debugLog *log.Logger

func main() {
	if os.Getppid() == 1 {
		log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))
	} else {
		log.SetFlags(log.Lshortfile | log.LstdFlags)
	}

	flag.Parse()

	if *debug {
		debugLog = log.New(os.Stderr, "", log.Flags())
	} else {
		debugLog = log.New(io.Discard, "", log.Flags())
	}

	var err error
	inf, err = net.InterfaceByName(*iface)
	if err != nil {
		log.Printf("multicast disabled: %v", err)
	}

	var mux http.ServeMux
	mux.HandleFunc("/r/", handleHLS)
	mux.HandleFunc("/rtp/", handleRTP)
	if *debug {
		mux.HandleFunc("/debug/pprof/", http.DefaultServeMux.ServeHTTP)
	}
	SdNotify("READY=1")
	go func() {
		for {
			updateStatus()
			time.Sleep(5 * time.Second)
		}
	}()
	log.Printf("Listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, &mux))
}
