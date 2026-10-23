package serrors

// String is only one public type which should be used in const as error
type String string

// New returns new sentinel error. We assume that New() called only once per error
// Note: if two errors has different arguments but same type `errors.Is` will return true
func (e String) New(args ...interface{}) sentinelError {
	return sentinelError{eString: &e, args: args}
}

// Error implements error interface
func (e String) Error() string {
	return e.String()
}

// String implements fmt.Stringer interface
func (e String) String() string {
	return *(*string)(&e)
}
