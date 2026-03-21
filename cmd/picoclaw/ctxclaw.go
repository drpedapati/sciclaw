package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const minimumCtxclawVersion = "v0.1.1"

var ctxclawVersionPattern = regexp.MustCompile(`\bv(\d+)\.(\d+)\.(\d+)\b`)

type ctxclawVersionInfo struct {
	Path       string
	Raw        string
	Parsed     string
	Compatible bool
	DevBuild   bool
}

func inspectCtxclawBinary(path string) (ctxclawVersionInfo, error) {
	info := ctxclawVersionInfo{Path: strings.TrimSpace(path)}
	out, err := runCommandWithTimeout(3*time.Second, info.Path, "version")
	if err != nil {
		return info, err
	}
	info.Raw = strings.TrimSpace(out)
	if strings.Contains(strings.ToLower(info.Raw), "dev") {
		info.DevBuild = true
		info.Compatible = true
		return info, nil
	}
	parsed, ok := extractSemver(info.Raw)
	if !ok {
		return info, fmt.Errorf("could not parse version from %q", info.Raw)
	}
	info.Parsed = parsed
	info.Compatible = compareSemver(info.Parsed, minimumCtxclawVersion) >= 0
	return info, nil
}

func extractSemver(s string) (string, bool) {
	m := ctxclawVersionPattern.FindStringSubmatch(strings.TrimSpace(s))
	if len(m) != 4 {
		return "", false
	}
	return fmt.Sprintf("v%s.%s.%s", m[1], m[2], m[3]), true
}

func compareSemver(a, b string) int {
	parse := func(s string) ([3]int, bool) {
		m := ctxclawVersionPattern.FindStringSubmatch(strings.TrimSpace(s))
		if len(m) != 4 {
			return [3]int{}, false
		}
		var out [3]int
		for i := 0; i < 3; i++ {
			n, err := strconv.Atoi(m[i+1])
			if err != nil {
				return [3]int{}, false
			}
			out[i] = n
		}
		return out, true
	}
	av, okA := parse(a)
	bv, okB := parse(b)
	switch {
	case !okA && !okB:
		return 0
	case !okA:
		return -1
	case !okB:
		return 1
	}
	for i := 0; i < 3; i++ {
		switch {
		case av[i] < bv[i]:
			return -1
		case av[i] > bv[i]:
			return 1
		}
	}
	return 0
}

func runCommandWithTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	timer := time.AfterFunc(timeout, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	defer timer.Stop()
	out, err := cmd.CombinedOutput()
	return string(out), err
}
