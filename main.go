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

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("accept: %v", err)
				shutdown <- nil
				return
			}

			forwardConn, err := net.Dial("tcp", forward)
			if err != nil {
				log.Printf("dial: %v", err)
				if err := conn.Close(); err != nil {
					log.Printf("conn.close: %v", err)
				}
				continue
			}

			go func() {
				var buf [1024]byte
				for {
					start := time.Now()
					size, err := conn.Read(buf[:])
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
				var buf [1024]byte
				for {
					start := time.Now()
					size, err := forwardConn.Read(buf[:])
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
	}()

	<-shutdown
	err = listener.Close()
	if err != nil {
		log.Printf("close: %v", err)
	}
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
