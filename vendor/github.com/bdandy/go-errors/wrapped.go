package serrors

import (
	"runtime"
	"strconv"
	"strings"
)

var WrapSeparator = ": "

type Wrapped interface {
	Unwrap() error
}

// wrapped is custom error type for better error handling
type wrapped struct {
	error
	cause     error
	stackFunc func() string
}

// Wrap adds cause to the error and return new wrapped error
func (e wrapped) Wrap(cause error) wrapped {
	return Wrap(e, cause)
}

func (e wrapped) WithStack() wrapped {
	return WrapWithStack(e.error, e.cause)
}

// Unwrap implements Wrapped interface
func (e wrapped) Unwrap() error {
	return e.cause
}

// Is implements Comparer interface
// Check if errors in chain match provided error
func (e wrapped) Is(err error) bool {
	if compare(e.error, err) || compare(e.cause, err) {
		return true
	}

	return false
}

func compare(err, cause error) bool {
	if cmp, ok := err.(Comparer); ok && cmp.Is(cause) {
		return true
	}

	if wrp, ok := err.(Wrapped); ok && compare(wrp.Unwrap(), cause) {
		return true
	}

	return false
}

// Error implements error interface; prints both error and cause
func (e wrapped) Error() string {
	var builder strings.Builder
	builder.WriteString(e.error.Error())

	if e.cause != nil {
		builder.WriteString(WrapSeparator)
		builder.WriteString(e.cause.Error())
	}

	return builder.String()
}

func (e wrapped) withStack() wrapped {
	if e.stackFunc == nil {
		var callers [1 << 5]uintptr

		runtime.Callers(4, callers[:])
		e.stackFunc = func() string {
			frames := runtime.CallersFrames(callers[:])

			var builder strings.Builder
			for {
				frame, more := frames.Next()
				builder.WriteString(frame.Function)
				builder.WriteString("\n\t")
				builder.WriteString(frame.File)
				builder.WriteString(":")
				builder.WriteString(strconv.Itoa(frame.Line))
				builder.WriteString("\n")
				if !more {
					break
				}
			}

			return builder.String()
		}
	}

	return e
}

// Stack implements Comparer
// Stack returns original stack which was placed to wrapped error
func (e wrapped) Stack() string {
	if e.stackFunc != nil {
		return e.stackFunc()
	}

	return ""
}

// Wrap wraps an error, adds its cause and stack trace
func Wrap(err, cause error) wrapped {
	return wrapped{error: err, cause: cause}
}

// WrapWithStack wraps cause with err and stores stack trace
func WrapWithStack(err, cause error) wrapped {
	return Wrap(err, cause).withStack()
}

// Stack returns stack if err is wrapped or implements Stack() string method
func Stack(err error) string {
	we, ok := err.(interface{ Stack() string })
	if !ok {
		return ""
	}

	return we.Stack()
}
