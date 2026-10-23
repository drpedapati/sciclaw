# Gorilla WebSocket

[![GoDoc](https://godoc.org/github.com/bogdanfinn/websocket?status.svg)](https://godoc.org/github.com/bogdanfinn/websocket)
[![CircleCI](https://circleci.com/gh/gorilla/websocket.svg?style=svg)](https://circleci.com/gh/gorilla/websocket)

Gorilla WebSocket is a [Go](http://golang.org/) implementation of the
[WebSocket](http://www.rfc-editor.org/rfc/rfc6455.txt) protocol.


### Documentation

* [API Reference](https://pkg.go.dev/github.com/bogdanfinn/websocket?tab=doc)
* [Chat example](https://github.com/bogdanfinn/websocket/tree/main/examples/chat)
* [Command example](https://github.com/bogdanfinn/websocket/tree/main/examples/command)
* [Client and server example](https://github.com/bogdanfinn/websocket/tree/main/examples/echo)
* [File watch example](https://github.com/bogdanfinn/websocket/tree/main/examples/filewatch)

### Status

The Gorilla WebSocket package provides a complete and tested implementation of
the [WebSocket](http://www.rfc-editor.org/rfc/rfc6455.txt) protocol. The
package API is stable.

### Installation

    go get github.com/bogdanfinn/websocket

### Protocol Compliance

The Gorilla WebSocket package passes the server tests in the [Autobahn Test
Suite](https://github.com/crossbario/autobahn-testsuite) using the application in the [examples/autobahn
subdirectory](https://github.com/bogdanfinn/websocket/tree/main/examples/autobahn).
