package metricsink

func Label(string)               {}
func Positioned(any, any)        {}
func Variadic(any, ...any)       {}
func All(...any)                 {}
func Generic[T any](T)           {}
func GenericPair[A, B any](A, B) {}

type Meter struct{}

func (Meter) Record(string, any)         {}
func (*Meter) PointerRecord(string, any) {}
