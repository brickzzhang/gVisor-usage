package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"gvisor.dev/gvisor/pkg/tcpip/link/tun"

	"github.com/brickzzhang/gVisor-usage/chandev"
)

var tap = flag.Bool("tap", false, "use tap instead of tun")

func echo2Uds(tunFD *os.File, destConn net.Conn) {
	var (
		packet = make([]byte, 4096)
		err    error
	)
	for {
		if _, err = tunFD.Read(packet); err != nil {
			log.Printf("read from tun error: %+v", err)
			return
		}

		if _, err = destConn.Write(packet); err != nil {
			log.Printf("write to uds error: %+v", err)
			return
		}
		clear(packet)
	}
}

func echo2Tun(tunFD *os.File, srcConn net.Conn) {
	var (
		packet = make([]byte, 4096)
		err    error
	)
	for {
		if _, err = srcConn.Read(packet); err != nil {
			if err == io.EOF {
				log.Printf("conn closed, read eof")
				return
			}
			log.Printf("read from uds error: %+v", err)
			return
		}

		if _, err = tunFD.Write(packet); err != nil {
			log.Printf("write to tun error: %+v", err)
			return
		}
		clear(packet)
	}
}

func udsClient(protocol, sockAddr string) (net.Conn, error) {
	// return unet.Connect(gvisorudshttp.UdsAddr, true)
	return net.Dial(chandev.UnixProto, chandev.UdsAddr)
}

func main() {
	flag.Parse()

	tunName := flag.Arg(0)

	var (
		fd  int
		err error
	)
	log.Printf("tap: %+v", *tap)
	if *tap {
		fd, err = tun.OpenTAP(tunName)
	} else {
		fd, err = tun.Open(tunName)
	}
	if err != nil {
		log.Fatal(err)
	}

	// start uds client
	udsConn, errUds := udsClient(chandev.UnixProto, chandev.UdsAddr)
	if errUds != nil {
		log.Printf("init uds client error: %+v", errUds)
		return
	}

	f := os.NewFile(uintptr(fd), tunName)
	go echo2Uds(f, udsConn)
	go echo2Tun(f, udsConn)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("sig received, exiting: %+v", sig)
}
