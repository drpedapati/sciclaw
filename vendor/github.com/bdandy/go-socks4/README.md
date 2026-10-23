# go-socks4
Socks4 implementation for Go, compatible with net/proxy. 

## Features
- `socks4` 
- `socks4a`

## Usage
Just import `_ "github.com/bdandy/go-socks4"` to add `socks4` support

```go
package main

import (
	"errors"
	"log"
	"net/url"

	_ "github.com/bdandy/go-socks4"
	"golang.org/x/net/proxy"
)

func main() {
	addr, _ := url.Parse("socks4://ip:port")

	dialer, err := proxy.FromURL(addr, proxy.Direct)
	conn, err := dialer.Dial("tcp", "google.com:80")
	if err != nil {
		// handle error
		if errors.Is(err, socks4.ErrDialFailed) {
			log.Printf("invalid proxy server %s", addr)
			return
		}
		if errors.Is(err, socks4.ErrConnRejected) {
			log.Printf("google.com:80: %s", err)
			return
		}
	}
	// use opened network connection
	_ = conn
}
```


## Tests
If you know proxy server to connect to tests should be running like this
`
go test -socks4.url=socks4://localhost:8080
`




