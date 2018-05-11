package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"time"
)

func main() {
	if len(os.Args) != 4 {
		printUsageAndExit("expected exactly 3 arguments")
	}

	listen := os.Args[1]
	forward := os.Args[2]
	throughput, err := strconv.Atoi(os.Args[3])
	if err != nil {
		printUsageAndExit(fmt.Sprintf("%s is not an integer", os.Args[3]))
	}

	shutdown := make(chan os.Signal)
	signal.Notify(shutdown, os.Interrupt, os.Kill)

	listener, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	go acceptConnections(listener, shutdown, forward, throughput)

	<-shutdown
	err = listener.Close()
	if err != nil {
		log.Printf("close: %v", err)
	}
}

func acceptConnections(listener net.Listener, shutdown chan os.Signal, forward string, throughput int) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			shutdown <- nil
			return
		}

		handleConnection(conn, forward, throughput)
	}
}

func handleConnection(conn net.Conn, forward string, throughput int) {
	// set the buffer size to the throughput (bytes/second) because it does not make sense to read more than
	// one second worth of data ahead
	bufSize := throughput

	if connTcp, ok := conn.(*net.TCPConn); ok {
		connTcp.SetReadBuffer(bufSize)
		connTcp.SetWriteBuffer(bufSize)
	} else {
		panic(fmt.Sprint("non-TCPConn connection: ", conn))
	}

	forwardConn, err := net.Dial("tcp", forward)
	if err != nil {
		log.Printf("dial: %v", err)
		if err := conn.Close(); err != nil {
			log.Printf("conn.close: %v", err)
		}
		return
	}
	if connTcp, ok := forwardConn.(*net.TCPConn); ok {
		connTcp.SetReadBuffer(bufSize)
		connTcp.SetWriteBuffer(bufSize)
	} else {
		panic(fmt.Sprint("non-TCPConn connection: ", forwardConn))
	}

	go func() {
		buf := make([]byte, bufSize, bufSize)
		for {
			start := time.Now()
			size, err := conn.Read(buf)
			if err != nil {
				log.Printf("conn.read: %v", err)
				conn.Close()
				forwardConn.Close()
				return
			}

			_, err = forwardConn.Write(buf[0:size])
			if err != nil {
				log.Printf("forwardConn.write: %v", err)
				conn.Close()
				forwardConn.Close()
				return
			}

			delay(throughput, size, time.Since(start))
		}
	}()

	go func() {
		buf := make([]byte, bufSize, bufSize)
		for {
			start := time.Now()
			size, err := forwardConn.Read(buf)
			if err != nil {
				log.Printf("forwardConn.read: %v", err)
				conn.Close()
				forwardConn.Close()
				return
			}

			_, err = conn.Write(buf[0:size])
			if err != nil {
				log.Printf("conn.write: %v", err)
				conn.Close()
				forwardConn.Close()
				return
			}

			delay(throughput, size, time.Since(start))
		}
	}()
}

func delay(throughput, size int, actualDelay time.Duration) {
	// calculate the relative number of bytes in relation to the allowed throughput
	share := float64(size) / float64(throughput)

	// calculate how long that should have taken
	expectedDelay := time.Duration(share*1000.0) * time.Millisecond

	// Sleep the remaining amount of time if necessary
	if actualDelay < expectedDelay {
		time.Sleep(expectedDelay - actualDelay)
	}
}

func printUsageAndExit(msg string) {
	log.Fatalf(`Usage: %s LISTEN FORWARD THROUGHPUT

  LISTEN      The listen address, eg. localhost:8080
  FORWARD     The forward address, eg. localhost:80
  THROUGHPUT  Maximum throughput in bytes per second

Error: %s`, os.Args[0], msg)
}
