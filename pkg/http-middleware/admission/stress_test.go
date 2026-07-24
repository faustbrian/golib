package admission_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/http-middleware/admission"
)

func TestOverloadStormStaysBoundedAndRecovers(t *testing.T) {
	t.Parallel()
	const limit = 8
	middleware, err := admission.New(admission.Policy{MaxInFlight: limit})
	if err != nil {
		t.Fatal(err)
	}
	var active, peak atomic.Int64
	entered := make(chan struct{}, limit)
	release := make(chan struct{})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := peak.Load()
			if current <= previous || peak.CompareAndSwap(previous, current) {
				break
			}
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))

	var group sync.WaitGroup
	statuses := make(chan int, 512)
	for range cap(statuses) {
		group.Add(1)
		go func() {
			defer group.Done()
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			statuses <- recorder.Code
		}()
	}
	for range limit {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("admission capacity did not fill")
		}
	}
	close(release)
	group.Wait()
	close(statuses)
	if peak.Load() > limit || active.Load() != 0 {
		t.Fatalf("peak = %d, active = %d", peak.Load(), active.Load())
	}
	accepted := 0
	for status := range statuses {
		if status == http.StatusNoContent {
			accepted++
		}
	}
	if accepted < limit {
		t.Fatalf("accepted = %d, want at least %d", accepted, limit)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("post-storm status = %d", recorder.Code)
	}
}

func TestFiniteWaiterWaveMakesProgressWithoutOrderGuarantee(t *testing.T) {
	t.Parallel()
	middleware, err := admission.New(admission.Policy{
		MaxInFlight: 1,
		MaxWaiters:  3,
		Wait:        time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	holderEntered := make(chan struct{})
	releaseHolder := make(chan struct{})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Waiter") == "holder" {
			close(holderEntered)
			<-releaseHolder
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	holderDone := make(chan struct{})
	go func() {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("X-Waiter", "holder")
		handler.ServeHTTP(httptest.NewRecorder(), request)
		close(holderDone)
	}()
	<-holderEntered

	statuses := make(chan int, 3)
	for range 3 {
		go func() {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			statuses <- recorder.Code
		}()
	}
	close(releaseHolder)
	<-holderDone
	for range 3 {
		select {
		case status := <-statuses:
			if status != http.StatusNoContent {
				t.Fatalf("waiter status = %d", status)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("finite waiter wave starved")
		}
	}
}
