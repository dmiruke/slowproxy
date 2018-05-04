# slowproxy
Slowproxy allows to slow down a TCP connection to a certain throughput.
This can be useful in testing the resilience of applications towards slow networks or
slow downstream services.

## Building
```golang
go build .
```

## Running
```bash
Usage: ./slowproxy LISTEN FORWARD THROUGHPUT

  LISTEN      The listen address, eg. localhost:8080
  FORWARD     The forward address, eg. localhost:80
  THROUGHPUT  Maximum throughput in bytes per second
```
