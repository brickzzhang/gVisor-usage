package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/waiter"

	"github.com/brickzzhang/gVisor-usage/chandev"
)

var (
	mac = flag.String("mac", "aa:00:01:01:01:01", "mac address to use in tap device")
)

func newEP(size int, mtu uint32, mac string) (*channel.Endpoint, error) {
	linkAddr, err := tcpip.ParseMACAddress(mac)
	if err != nil {
		return nil, err
	}

	ep := channel.New(size, mtu, linkAddr)
	return ep, nil
}

func startHTTPServer(
	linkEp *channel.Endpoint, addrWithPrefix tcpip.AddressWithPrefix, proto tcpip.NetworkProtocolNumber, localPort int,
) error {
	// Create the stack with ip and tcp protocols, then add a tun-based
	// NIC and address.
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol, arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol},
	})

	if err := s.CreateNIC(1, linkEp); err != nil {
		log.Printf("create nic error: %+v", err)
		panic(err)
	}

	protocolAddr := tcpip.ProtocolAddress{
		Protocol:          proto,
		AddressWithPrefix: addrWithPrefix,
	}
	if err := s.AddProtocolAddress(1, protocolAddr, stack.AddressProperties{}); err != nil {
		log.Fatalf("AddProtocolAddress(%d, %+v, {}): %s", 1, protocolAddr, err)
	}

	subnet, err := tcpip.NewSubnet(tcpip.AddrFromSlice([]byte(strings.Repeat("\x00", addrWithPrefix.Address.Len()))), tcpip.MaskFrom(strings.Repeat("\x00", addrWithPrefix.Address.Len())))
	if err != nil {
		log.Printf("new subnet error: %+v", err)
		panic(err)
	}

	// Add default route.
	s.SetRouteTable([]tcpip.Route{
		{
			Destination: subnet,
			NIC:         1,
		},
	})

	// Create TCP endpoint, bind it, then start listening.
	var wq waiter.Queue
	ep, e := s.NewEndpoint(tcp.ProtocolNumber, proto, &wq)
	if e != nil {
		log.Printf("new endpoint error: %+v", e)
		panic(e)
	}
	defer ep.Close()

	addr := tcpip.FullAddress{
		NIC:      1,
		Addr:     addrWithPrefix.Address,
		Port:     uint16(localPort),
		LinkAddr: linkEp.LinkAddress(),
	}
	ln, err := gonet.ListenTCP(s, addr, proto)
	if err != nil {
		log.Printf("listen tcp for go net error: %+v", err)
		return err
	}
	defer ln.Close()

	// start http server
	m := http.NewServeMux()
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello kung fu developer! "))
	})

	server := http.Server{
		Handler: m,
	}

	return server.Serve(ln)
}

func echo2Ep(ep *channel.Endpoint, conn net.Conn, proto tcpip.NetworkProtocolNumber, cancel context.CancelFunc) {
	var (
		buf = make([]byte, 4096)
		err error
	)
	for {
		if _, err = conn.Read(buf); err != nil {
			if err == io.EOF {
				log.Printf("conn closed, read eof")
				// notify echo2Uds return
				cancel()
				return
			}
			log.Printf("read from unix socket conn error: %+v", err)
			break
		}

		stackBuf := buffer.MakeWithData(buf)
		pb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: stackBuf})
		ep.InjectInbound(proto, pb)

		clear(buf)
	}
}

func echo2Uds(ctx context.Context, ep *channel.Endpoint, conn net.Conn) {
	buf := &bytes.Buffer{}
	for {
		pkb := ep.ReadContext(ctx)
		if pkb == nil {
			log.Printf("recieve ctx done, return")
			return
		}
		pkbp := pkb.ToBuffer()
		leng, err := pkbp.ReadToWriter(buf, 4096)
		if err != nil {
			log.Printf("read from gvisor endpoint error: %+v", err)
			buf.Reset()
			continue
		}

		if _, err := conn.Write(buf.Bytes()[:leng]); err != nil {
			log.Printf("write to uds error: %+v", err)
			return
		}
		buf.Reset()
	}
}

func echo(ep *channel.Endpoint, conn net.Conn, proto tcpip.NetworkProtocolNumber) {
	ctx, cancel := context.WithCancel(context.Background())
	go echo2Uds(ctx, ep, conn)
	go echo2Ep(ep, conn, proto, cancel)
}

func main() {
	flag.Parse()

	addrName := flag.Arg(0)
	portName := flag.Arg(1)

	rand.New(rand.NewSource(time.Now().UnixNano()))

	// Cleanup the sockfile.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Remove(chandev.UdsAddr)
		os.Exit(1)
	}()

	// Parse the IP address. Support both ipv4 and ipv6.
	parsedAddr := net.ParseIP(addrName)
	if parsedAddr == nil {
		log.Fatalf("Bad IP address: %v", addrName)
	}

	var addrWithPrefix tcpip.AddressWithPrefix
	var proto tcpip.NetworkProtocolNumber
	if parsedAddr.To4() != nil {
		addrWithPrefix = tcpip.AddrFromSlice(parsedAddr.To4()).WithPrefix()
		proto = ipv4.ProtocolNumber
	} else if parsedAddr.To16() != nil {
		addrWithPrefix = tcpip.AddrFromSlice(parsedAddr.To16()).WithPrefix()
		proto = ipv6.ProtocolNumber
	} else {
		log.Fatalf("Unknown IP type: %v", addrName)
	}

	localPort, err := strconv.Atoi(portName)
	if err != nil {
		log.Fatalf("Unable to convert port %v: %v", portName, err)
	}

	// create endpoint for gvisor, used to send package for each unix socket connection
	linkEp, err := newEP(100, 4096, *mac)
	if err != nil {
		log.Printf("create endpoint error: %+v", err)
		panic(err)
	}
	defer linkEp.Close()

	// start http server bases on givosr stack
	go func() {
		if err := startHTTPServer(linkEp, addrWithPrefix, proto, localPort); err != nil {
			log.Printf("start http server error: %+v", err)
			panic(err)
		}
	}()

	// Create a Unix domain socket and listen for incoming connections.
	ln, err := net.Listen(chandev.UnixProto, chandev.UdsAddr)
	if err != nil {
		log.Printf("listen unix socket error: %+v", err)
		panic(err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept unix socket error: %+v", err)
			panic(err)
		}
		go func() {
			echo(linkEp, conn, proto)
		}()
	}
}
