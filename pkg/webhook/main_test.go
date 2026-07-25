package webhook

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(testingMain *testing.M) {
	goleak.VerifyTestMain(testingMain, goleak.IgnoreCurrent())
}
