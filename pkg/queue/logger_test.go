package queue

import (
	"log"
	"testing"
)

func TestNewLoggerUsesDateAndTimeForEveryLevel(t *testing.T) {
	t.Parallel()

	logger, ok := NewLogger().(defaultLogger)
	if !ok {
		t.Fatalf("NewLogger() type = %T, want defaultLogger", NewLogger())
	}
	want := log.Ldate | log.Ltime
	for name, actual := range map[string]int{
		"info":  logger.infoLogger.Flags(),
		"error": logger.errorLogger.Flags(),
		"fatal": logger.fatalLogger.Flags(),
	} {
		if actual != want {
			t.Errorf("%s logger flags = %d, want %d", name, actual, want)
		}
	}
}

func ExampleNewEmptyLogger() {
	l := NewEmptyLogger()
	l.Info("test")
	l.Infof("test")
	l.Error("test")
	l.Errorf("test")
	l.Fatal("test")
	l.Fatalf("test")
	// Output:
}
