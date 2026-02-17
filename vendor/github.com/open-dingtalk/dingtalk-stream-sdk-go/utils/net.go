package utils

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

func GetFirstLanIP() (string, error) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	if len(addresses) == 0 {
		return "", errors.New("empty interfaces")
	}

	for _, addr := range addresses {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}
	return "", errors.New("no valid interfaces")
}

func DumpHeaders(h http.Header) string {
	var lines []string
	for name, values := range h {
		for _, value := range values {
			lines = append(lines, fmt.Sprintf("%s: %s", name, value))
		}
	}
	return strings.Join(lines, "\n")
}
