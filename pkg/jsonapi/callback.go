package jsonapi

// CallbackError contains a failure from an application-supplied validator.
// Error deliberately omits the callback's error or panic value. Cause and
// PanicValue remain available for explicit diagnostics.
type CallbackError struct {
	Phase      string
	Cause      error
	PanicValue any
	panicked   bool
}

// Error implements error without disclosing application-owned values.
func (err *CallbackError) Error() string {
	return "JSON:API application callback failed during " + err.Phase
}

// Unwrap exposes an error returned by or panicked from the callback.
func (err *CallbackError) Unwrap() error {
	return err.Cause
}

// CallbackPhase identifies the callback seam that failed.
func (err *CallbackError) CallbackPhase() string {
	return err.Phase
}

// CallbackPanicValue returns the panic value and whether the callback panicked.
func (err *CallbackError) CallbackPanicValue() (any, bool) {
	return err.PanicValue, err.panicked
}

func callApplicationCallback(phase string, callback func() error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			cause, _ := value.(error)
			err = &CallbackError{
				Phase: phase, Cause: cause, PanicValue: value, panicked: true,
			}
		}
	}()
	if cause := callback(); cause != nil {
		return &CallbackError{Phase: phase, Cause: cause}
	}
	return nil
}
