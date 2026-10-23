package socks4

import (
	"bytes"
	"encoding/binary"
	"net"
)

type request struct {
	Host string
	Port int
	IP   net.IP
	Is4a bool

	err error
	buf bytes.Buffer
}

func (r *request) write(b []byte) {
	if r.err == nil {
		_, r.err = r.buf.Write(b)
	}
}

func (r *request) writeString(s string) {
	if r.err == nil {
		_, r.err = r.buf.WriteString(s)
	}
}

func (r *request) writeBigEndian(data interface{}) {
	if r.err == nil {
		r.err = binary.Write(&r.buf, binary.BigEndian, data)
	}
}

func (r request) Bytes() ([]byte, error) {
	r.write([]byte{socksVersion, socksConnect})
	r.writeBigEndian(uint16(r.Port))
	r.writeBigEndian(r.IP.To4())
	r.writeString(Ident)
	r.write([]byte{0})
	if r.Is4a {
		r.writeString(r.Host)
		r.write([]byte{0})
	}

	return r.buf.Bytes(), r.err
}
