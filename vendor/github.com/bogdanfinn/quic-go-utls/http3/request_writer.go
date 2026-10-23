package http3

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	http "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/fhttp/httptrace"
	"github.com/quic-go/qpack"
	"golang.org/x/net/http/httpguts"
	"golang.org/x/net/http2/hpack"
	"golang.org/x/net/idna"

	"github.com/bogdanfinn/quic-go-utls"
	"github.com/bogdanfinn/quic-go-utls/http3/qlog"
	"github.com/bogdanfinn/quic-go-utls/qlogwriter"
)

const bodyCopyBufferSize = 8 * 1024

type requestWriter struct {
	mutex             sync.Mutex
	encoder           *qpack.Encoder
	headerBuf         *bytes.Buffer
	pseudoHeaderOrder []string
	priorityParam     uint32
}

func newRequestWriter() *requestWriter {
	headerBuf := &bytes.Buffer{}
	encoder := qpack.NewEncoder(headerBuf)
	return &requestWriter{
		encoder:   encoder,
		headerBuf: headerBuf,
		// Default Chrome-like order: method, authority, scheme, path
		pseudoHeaderOrder: []string{":method", ":authority", ":scheme", ":path"},
	}
}

func newRequestWriterWithPseudoHeaderOrder(pseudoHeaderOrder []string, priorityParam uint32) *requestWriter {
	headerBuf := &bytes.Buffer{}
	encoder := qpack.NewEncoder(headerBuf)
	order := pseudoHeaderOrder
	if order == nil || len(order) == 0 {
		// Default Chrome-like order: method, authority, scheme, path
		order = []string{":method", ":authority", ":scheme", ":path"}
	}
	return &requestWriter{
		encoder:           encoder,
		headerBuf:         headerBuf,
		pseudoHeaderOrder: order,
		priorityParam:     priorityParam,
	}
}

func (w *requestWriter) WriteRequestHeader(wr io.Writer, req *http.Request, gzip bool, streamID quic.StreamID, qlogger qlogwriter.Recorder) error {
	// TODO: figure out how to add support for trailers
	buf := &bytes.Buffer{}
	if err := w.writeHeaders(buf, req, gzip, streamID, qlogger); err != nil {
		return err
	}
	if _, err := wr.Write(buf.Bytes()); err != nil {
		return err
	}
	trace := httptrace.ContextClientTrace(req.Context())
	traceWroteHeaders(trace)
	return nil
}

func (w *requestWriter) writeHeaders(wr io.Writer, req *http.Request, gzip bool, streamID quic.StreamID, qlogger qlogwriter.Recorder) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	defer w.encoder.Close()
	defer w.headerBuf.Reset()

	headerFields, err := w.encodeHeaders(req, gzip, "", actualContentLength(req), qlogger != nil)
	if err != nil {
		return err
	}

	b := make([]byte, 0, 128)
	b = (&headersFrame{Length: uint64(w.headerBuf.Len())}).Append(b)
	if qlogger != nil {
		qlogCreatedHeadersFrame(qlogger, streamID, len(b)+w.headerBuf.Len(), w.headerBuf.Len(), headerFields)
	}
	if _, err := wr.Write(b); err != nil {
		return err
	}
	_, err = wr.Write(w.headerBuf.Bytes())
	return err
}

func isExtendedConnectRequest(req *http.Request) bool {
	return req.Method == http.MethodConnect && req.Proto != "" && req.Proto != "HTTP/1.1"
}

// copied from net/transport.go
// Modified to support Extended CONNECT:
// Contrary to what the godoc for the http.Request says,
// we do respect the Proto field if the method is CONNECT.
//
// The returned header fields are only set if doQlog is true.
func (w *requestWriter) encodeHeaders(req *http.Request, addGzipHeader bool, trailers string, contentLength int64, doQlog bool) ([]qlog.HeaderField, error) {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	host, err := httpguts.PunycodeHostPort(host)
	if err != nil {
		return nil, err
	}
	if !httpguts.ValidHostHeader(host) {
		return nil, errors.New("http3: invalid Host header")
	}

	// http.NewRequest sets this field to HTTP/1.1
	isExtendedConnect := isExtendedConnectRequest(req)

	var path string
	if req.Method != http.MethodConnect || isExtendedConnect {
		path = req.URL.RequestURI()
		if !validPseudoPath(path) {
			orig := path
			path = strings.TrimPrefix(path, req.URL.Scheme+"://"+host)
			if !validPseudoPath(path) {
				if req.URL.Opaque != "" {
					return nil, fmt.Errorf("invalid request :path %q from URL.Opaque = %q", orig, req.URL.Opaque)
				} else {
					return nil, fmt.Errorf("invalid request :path %q", orig)
				}
			}
		}
	}

	// Check for any invalid headers and return an error before we
	// potentially pollute our hpack state. (We want to be able to
	// continue to reuse the hpack encoder for future requests)
	for k, vv := range req.Header {
		if !httpguts.ValidHeaderFieldName(k) && k != ":protocol" {
			if k == http.HeaderOrderKey || k == http.PHeaderOrderKey {
				continue
			}

			return nil, fmt.Errorf("invalid HTTP header name %q", k)
		}
		for _, v := range vv {
			if !httpguts.ValidHeaderFieldValue(v) {
				return nil, fmt.Errorf("invalid HTTP header value %q for header %q", v, k)
			}
		}
	}

	enumerateHeaders := func(f func(name, value string)) {
		// 8.1.2.3 Request Pseudo-Header Fields
		// Send pseudo-headers in the configured order
		pseudoHeaders := map[string]string{
			":authority": host,
			":method":    req.Method,
		}
		if req.Method != http.MethodConnect || isExtendedConnect {
			pseudoHeaders[":path"] = path
			pseudoHeaders[":scheme"] = req.URL.Scheme
		}
		if isExtendedConnect {
			pseudoHeaders[":protocol"] = req.Proto
		}

		// Write pseudo-headers in the configured order
		for _, name := range w.pseudoHeaderOrder {
			if value, ok := pseudoHeaders[name]; ok {
				f(name, value)
				delete(pseudoHeaders, name) // Mark as written
			}
		}
		// Write any remaining pseudo-headers (e.g., :protocol for extended CONNECT)
		for name, value := range pseudoHeaders {
			f(name, value)
		}

		// Normalize field names once.
		headers := make(http.Header, len(req.Header)+5)
		for name, values := range req.Header {
			if name == http.HeaderOrderKey || name == http.PHeaderOrderKey {
				// Skip magic ordering keys.
				continue
			}
			name = strings.ToLower(name)
			headers[name] = append(headers[name], values...)
		}

		// Add all automatically generated fields before sorting.
		delete(headers, "content-length")
		if shouldSendReqContentLength(req.Method, contentLength) {
			headers["content-length"] = []string{strconv.FormatInt(contentLength, 10)}
		}
		if _, ok := headers["accept-encoding"]; !ok && addGzipHeader {
			headers["accept-encoding"] = []string{"gzip"}
		}
		if _, ok := headers["priority"]; !ok && w.priorityParam > 0 {
			// Send standard Chrome priority format if priorityParam is set (RFC 9218).
			headers["priority"] = []string{"u=0, i"}
		}
		if _, ok := headers["trailer"]; !ok && trailers != "" {
			headers["trailer"] = []string{trailers}
		}
		// Match Go's http1 behavior: at most one
		// User-Agent. If set to nil or empty string,
		// then omit it. Otherwise if not mentioned,
		// include the default.
		if values, ok := headers["user-agent"]; ok {
			if len(values) == 0 || values[0] == "" {
				delete(headers, "user-agent")
			} else {
				headers["user-agent"] = values[:1]
			}
		} else {
			headers["user-agent"] = []string{defaultUserAgent}
		}

		var order map[string]int
		if headerOrder, ok := req.Header[http.HeaderOrderKey]; ok {
			order = make(map[string]int, len(headerOrder))
			for i, name := range headerOrder {
				order[strings.ToLower(name)] = i
			}
		}
		sortedHeaders, _ := headers.SortedKeyValuesBy(order, nil)

		for _, kv := range sortedHeaders {
			k, vv := kv.Key, kv.Values
			if len(vv) == 0 {
				continue
			}

			if k == "host" {
				// Host is :authority, already sent.
				continue
			} else if k == "connection" || k == "proxy-connection" || k == "transfer-encoding" ||
				k == "upgrade" || k == "keep-alive" {
				// Per 8.1.2.2 Connection-Specific Header
				// Fields, don't send connection-specific
				// fields. We have already checked if any
				// are error-worthy so just ignore the rest.
				continue
			}

			for _, v := range vv {
				f(k, v)
			}
		}
	}

	// Do a first pass over the headers counting bytes to ensure
	// we don't exceed cc.peerMaxHeaderListSize. This is done as a
	// separate pass before encoding the headers to prevent
	// modifying the hpack state.
	hlSize := uint64(0)
	enumerateHeaders(func(name, value string) {
		hf := hpack.HeaderField{Name: name, Value: value}
		hlSize += uint64(hf.Size())
	})

	// TODO: check maximum header list size
	// if hlSize > cc.peerMaxHeaderListSize {
	// 	return errRequestHeaderListSize
	// }

	trace := httptrace.ContextClientTrace(req.Context())
	traceHeaders := traceHasWroteHeaderField(trace)

	// Header list size is ok. Write the headers.
	var headerFields []qlog.HeaderField
	if doQlog {
		headerFields = make([]qlog.HeaderField, 0, len(req.Header))
	}
	enumerateHeaders(func(name, value string) {
		name = strings.ToLower(name)
		w.encoder.WriteField(qpack.HeaderField{Name: name, Value: value})
		if traceHeaders {
			traceWroteHeaderField(trace, name, value)
		}
		if doQlog {
			headerFields = append(headerFields, qlog.HeaderField{Name: name, Value: value})
		}
	})

	return headerFields, nil
}

// authorityAddr returns a given authority (a host/IP, or host:port / ip:port)
// and returns a host:port. The port 443 is added if needed.
func authorityAddr(authority string) (addr string) {
	host, port, err := net.SplitHostPort(authority)
	if err != nil { // authority didn't have a port
		port = "443"
		host = authority
	}
	if a, err := idna.ToASCII(host); err == nil {
		host = a
	}
	// IPv6 address literal, without a port:
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host + ":" + port
	}
	return net.JoinHostPort(host, port)
}

// validPseudoPath reports whether v is a valid :path pseudo-header
// value. It must be either:
//
//	*) a non-empty string starting with '/'
//	*) the string '*', for OPTIONS requests.
//
// For now this is only used a quick check for deciding when to clean
// up Opaque URLs before sending requests from the Transport.
// See golang.org/issue/16847
//
// We used to enforce that the path also didn't start with "//", but
// Google's GFE accepts such paths and Chrome sends them, so ignore
// that part of the spec. See golang.org/issue/19103.
func validPseudoPath(v string) bool {
	return (len(v) > 0 && v[0] == '/') || v == "*"
}

// actualContentLength returns a sanitized version of
// req.ContentLength, where 0 actually means zero (not unknown) and -1
// means unknown.
func actualContentLength(req *http.Request) int64 {
	if req.Body == nil {
		return 0
	}
	if req.ContentLength != 0 {
		return req.ContentLength
	}
	return -1
}

// shouldSendReqContentLength reports whether the http2.Transport should send
// a "content-length" request header. This logic is basically a copy of the net/http
// transferWriter.shouldSendContentLength.
// The contentLength is the corrected contentLength (so 0 means actually 0, not unknown).
// -1 means unknown.
func shouldSendReqContentLength(method string, contentLength int64) bool {
	if contentLength > 0 {
		return true
	}
	if contentLength < 0 {
		return false
	}
	// For zero bodies, whether we send a content-length depends on the method.
	// It also kinda doesn't matter for http2 either way, with END_STREAM.
	switch method {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}
