package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"sync/atomic"
)

func updateStatus() {
	rstream, rclient := rtpServer.Count()
	hclient := atomic.LoadInt64(&hlsClient)
	if hclient > 0 && rstream > 0 {
		SdNotify(fmt.Sprintf("STATUS=RTP(stream: %d, client: %d), HLS(client: %d)", rstream, rclient, hclient))
	} else if rstream > 0 {
		SdNotify(fmt.Sprintf("STATUS=RTP(stream: %d, client: %d)", rstream, rclient))
	} else if hclient > 0 {
		SdNotify(fmt.Sprintf("STATUS=HLS(client: %d)", hclient))
	} else {
		SdNotify("STATUS=Idle")
	}
}

// SdNotify
// steal from https://github.com/coreos/go-systemd/blob/main/daemon/sdnotify.go
func SdNotify(state string) (bool, error) {
	socketAddr := &net.UnixAddr{
		Name: os.Getenv("NOTIFY_SOCKET"),
		Net:  "unixgram",
	}

	// NOTIFY_SOCKET not set
	if socketAddr.Name == "" {
		log.Printf("NOTIFY_SOCKET not found")
		return false, nil
	}

	conn, err := net.DialUnix(socketAddr.Net, nil, socketAddr)
	// Error connecting to NOTIFY_SOCKET
	if err != nil {
		return false, err
	}
	defer conn.Close()

	if _, err = conn.Write([]byte(state)); err != nil {
		return false, err
	}
	return true, nil
}
