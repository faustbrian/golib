package routertest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	router "github.com/faustbrian/golib/pkg/router"
)

type recordingT struct {
	failures int
}

func (*recordingT) Helper() {}
func (testingT *recordingT) Fatalf(string, ...any) {
	testingT.failures++
}

func TestHelperFailureContracts(t *testing.T) {
	t.Parallel()

	testingT := &recordingT{}
	if MustCompile(testingT, nil) != nil || testingT.failures != 1 {
		t.Fatal("MustCompile failure contract changed")
	}
	if Serve(testingT, http.NotFoundHandler(), "bad method", "://") != nil || testingT.failures != 2 {
		t.Fatal("Serve failure contract changed")
	}
	AssertStatus(testingT, nil, 200)
	AssertStatus(testingT, httptest.NewRecorder(), 204)
	if testingT.failures != 4 {
		t.Fatalf("AssertStatus failures: %d", testingT.failures)
	}
	if RouteTable(testingT, nil) != nil || testingT.failures != 5 {
		t.Fatal("RouteTable failure contract changed")
	}

	builder := router.New()
	compiled, err := builder.Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if RouteTable(testingT, compiled) == nil {
		t.Fatal("empty route table must be a non-nil snapshot")
	}
}
