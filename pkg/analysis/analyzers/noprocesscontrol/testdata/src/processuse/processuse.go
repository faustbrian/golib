package processuse

import (
	"fmt"
	logalias "log"
	osalias "os"
)

func Panic() {
	panic("boom") // want `lifecycle/no-process-control: panic is restricted to approved entrypoints`
}

func GenericPanic[T any](value T) T {
	if any(value) == nil {
		panic("boom") // want `lifecycle/no-process-control: panic is restricted to approved entrypoints`
	}
	return value
}

func Fatal() {
	logalias.Fatal("boom") // want `lifecycle/no-process-control: log.Fatal is restricted to approved entrypoints`
}

func Fatalf() {
	logalias.Fatalf("%s", "boom") // want `lifecycle/no-process-control: log.Fatalf is restricted to approved entrypoints`
}

func Fatalln() {
	logalias.Fatalln("boom") // want `lifecycle/no-process-control: log.Fatalln is restricted to approved entrypoints`
}

func Exit() {
	osalias.Exit(1) // want `lifecycle/no-process-control: os.Exit is restricted to approved entrypoints`
}

func Shadowed() {
	panic := func(any) {}
	panic("safe")
}

type logger struct{}

func (logger) Fatal(string) {}

type callbacks struct {
	Fatal func()
}

func NearMiss() string {
	logger{}.Fatal("safe")
	callbacks{Fatal: func() {}}.Fatal()
	return fmt.Sprint("safe")
}
