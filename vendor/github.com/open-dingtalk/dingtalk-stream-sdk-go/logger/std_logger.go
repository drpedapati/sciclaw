package logger

import (
	"fmt"
	"time"
)

/**
 * @Author linya.jj
 * @Date 2023/3/22 14:32
 */

// This logger is only for debug. Do not use it online.
type StdTestLogger struct {
	isDebugEnabled bool
}

func NewStdTestLogger() *StdTestLogger {
	return &StdTestLogger{
		isDebugEnabled: false,
	}
}

func NewStdTestLoggerWithDebug() *StdTestLogger {
	return &StdTestLogger{
		isDebugEnabled: true,
	}
}

func (l *StdTestLogger) Debugf(format string, args ...interface{}) {
	if l.isDebugEnabled {
		fmt.Printf("%s [Debug] ", time.Now().String())
		fmt.Printf(format, args...)
		fmt.Print("\n")
	}
}

func (l *StdTestLogger) Infof(format string, args ...interface{}) {
	fmt.Printf("%s [INFO] ", time.Now().String())
	fmt.Printf(format, args...)
	fmt.Print("\n")
}

func (l *StdTestLogger) Warningf(format string, args ...interface{}) {
	fmt.Printf("%s [WARNING] ", time.Now().String())
	fmt.Printf(format, args...)
	fmt.Print("\n")
}

func (l *StdTestLogger) Errorf(format string, args ...interface{}) {
	fmt.Printf("%s [ERROR] ", time.Now().String())
	fmt.Printf(format, args...)
	fmt.Print("\n")
}

func (l *StdTestLogger) Fatalf(format string, args ...interface{}) {
	fmt.Printf("%s [FATAL] ", time.Now().String())
	fmt.Printf(format, args...)
	fmt.Print("\n")
}
