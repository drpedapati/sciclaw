//go:build darwin

package hardware

import "syscall"

func detectMemoryGB() int {
	val, err := syscall.Sysctl("hw.memsize")
	if err != nil || len(val) == 0 {
		return 0
	}
	// hw.memsize returns a binary uint64 in host byte order.
	// syscall.Sysctl strips trailing NUL bytes, so len(val) may be < 8;
	// missing high bytes are implicitly zero.
	var mem uint64
	for i := 0; i < len(val); i++ {
		mem |= uint64(val[i]) << (8 * i)
	}
	return int(mem / (1024 * 1024 * 1024))
}
