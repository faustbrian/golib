package sinkapi

func Record(any) {}

func Format(string, ...any) string { return "" }

func All(...any) {}

func RecordThree(any, any, any) {}

func Current(any) {}

func Capture[T any](T) {}

func CapturePair[T, U any](T, U) {}

type Logger struct{}

func (*Logger) Log(string, any) {}
