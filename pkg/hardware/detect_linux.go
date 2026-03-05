//go:build linux

package hardware

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func detectMemoryGB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return int(kb / (1024 * 1024))
				}
			}
			break
		}
	}
	return 0
}
