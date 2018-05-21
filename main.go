package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
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

	var shuttingDown uint32
	shutdown := make(chan os.Signal)
	signal.Notify(shutdown, os.Interrupt, os.Kill)

	listener, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	go server(listener, &shuttingDown, forward, throughput)

	<-shutdown
	atomic.StoreUint32(&shuttingDown, 1)
	err = listener.Close()
	if err != nil {
		log.Printf("close: %v", err)
	}
}

// server accepts new connections and forwards them accordingly to the forward address limiting the throughput (bytes
// per second). The integer shuttingDown is used as a flag to indicate that the process is shutting down.
func server(listener net.Listener, shuttingDown *uint32, forward string, throughput int) {
	for {
		incomingConn, err := listener.Accept()
		if atomic.LoadUint32(shuttingDown) != 0 { // if the process is shutting down we can ignore the error if any
			return
		}
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}

		// set the buffer size to the throughput (bytes/second) because it does not make sense to read more than
		// one second worth of data ahead
		bufSize := throughput

		forwardConn, err := net.Dial("tcp", forward)
		if err != nil {
			log.Printf("unable to dial: %v", err)
			if err := incomingConn.Close(); err != nil {
				log.Printf("%v: unexpected error: %v", incomingConn.RemoteAddr(), err)
			}
			continue
		}

		connTcp := incomingConn.(*net.TCPConn)
		forwardConnTcp := forwardConn.(*net.TCPConn)

		setTcpConnBuffers(connTcp, bufSize)
		setTcpConnBuffers(forwardConnTcp, bufSize)

		log.Print(connTcp.RemoteAddr(), " open")

		go slowCopy(forwardConnTcp, connTcp, throughput, bufSize)
		go slowCopy(connTcp, forwardConnTcp, throughput, bufSize)
	}
}

// setTcpConnBuffers adjusts the connection read and write buffer sizes to the specified value.
func setTcpConnBuffers(conn *net.TCPConn, bufSize int) {
	conn.SetReadBuffer(bufSize)
	conn.SetWriteBuffer(bufSize)
}

// slowCopy works like io.Copy but limits the throughput to the specified value (in bytes per second) and reads no more
// than bufSize at a time.
func slowCopy(w *net.TCPConn, r *net.TCPConn, throughput, bufSize int) {
	buf := make([]byte, bufSize, bufSize)
	for {
		start := time.Now()
		size, err := r.Read(buf)
		if err == io.EOF || isBrokenPipe(err) {
			log.Printf("%v: closed", r.RemoteAddr())
			w.CloseWrite()
			return
		}
		if err != nil {
			log.Printf("%v: unexpected error: %v", r.RemoteAddr(), err)
			w.Close()
			r.Close()
			return
		}

		_, err = w.Write(buf[0:size])
		if err == io.EOF || isBrokenPipe(err) {
			log.Printf("%v: closed", w.RemoteAddr())
			r.CloseRead()
			return
		}
		if err != nil {
			log.Printf("%v: unexpected error: %v", w.RemoteAddr(), err)
			w.Close()
			r.Close()
			return
		}

		delay(throughput, size, time.Since(start))
	}
}

// isBrokenPipe determines if err was caused by an EPIPE error.
func isBrokenPipe(err error) bool {
	opErr, ok := err.(*net.OpError)
	if !ok {
		return false
	}
	syscallErr, ok := opErr.Err.(*os.SyscallError)
	if !ok {
		return false
	}
	errno, ok := syscallErr.Err.(syscall.Errno)
	if !ok {
		return false
	}
	if errno == syscall.EPIPE {
		return true
	} else {
		log.Printf("errno: 0x%x", errno)
		return false
	}
}

// delay sleeps for the appropriate amount of time in order to simulate throughput. It requires the amount of
// transmitted data and the time it took (transmissionDuration) in order to calculate the pause time.
func delay(throughput, transmitted int, transmissionDuration time.Duration) {
	// calculate the relative number of bytes in relation to the allowed throughput
	share := float64(transmitted) / float64(throughput)

	// calculate how long that should have taken
	expectedDelay := time.Duration(share*1000.0) * time.Millisecond

	// Sleep the remaining amount of time if necessary
	if transmissionDuration < expectedDelay {
		time.Sleep(expectedDelay - transmissionDuration)
	}
}

func printUsageAndExit(msg string) {
	log.Fatalf(`Usage: %s LISTEN FORWARD THROUGHPUT

  LISTEN      The listen address, eg. localhost:8080
  FORWARD     The forward address, eg. localhost:80
  THROUGHPUT  Maximum throughput in bytes per second

Error: %s`, os.Args[0], msg)
}
