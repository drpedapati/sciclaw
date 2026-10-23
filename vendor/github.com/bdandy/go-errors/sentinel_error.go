// Package serrors provides better sentinel errors support
package serrors

import "fmt"

type Comparer interface {
	error
	Is(err error) bool // Is checks if Comparer and provided error has same type
}

type Error interface {
	Comparer
	Wrap(cause error) wrapped // Wrap wraps provided error
	WithStack() wrapped       // Stack stores call stack and returns wrapped error
}

type eString = String

// sentinelError implements Error interface
type sentinelError struct {
	*eString
	args []interface{}
}

// New call on sentinel error is unexpected as error is already initialized
func (sentinelError) New(args ...interface{}) sentinelError {
	panic("New() can be called only once")
}

// Error implements error interface
func (e sentinelError) Error() string {
	return e.String()
}

// String implements fmt.Stringer interface
func (e sentinelError) String() string {
	if len(e.args) == 0 {
		return e.eString.String()
	}

	return fmt.Sprintf(e.eString.String(), e.args...)
}

// Is implements Comparer
// err could be only base String type, as we are comparing with const
func (e sentinelError) Is(err error) bool {
	switch ee := err.(type) {
	case String:
		return *e.eString == ee
	}
	return false
}

// Wrap adds cause to the String error and return wrapped
func (e sentinelError) Wrap(cause error) wrapped {
	return Wrap(e, cause)
}

// WithStack implements Comparer interface
func (e sentinelError) WithStack() wrapped {
	return WrapWithStack(e, nil)
}
