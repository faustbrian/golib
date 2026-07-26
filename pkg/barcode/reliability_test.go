package barcode_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/code128"
	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
	"github.com/faustbrian/golib/pkg/barcode/qr"
	"github.com/faustbrian/golib/pkg/barcode/render"
)

func TestConcurrentEncodeRenderAndDecodeIsDeterministic(t *testing.T) {
	shared, err := qr.Encode([]byte("shared immutable symbol"), qr.Options{})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	wantModules := shared.Logical().Matrix().Modules()

	const workers = 16
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, encodeErr := code128.Encode([]byte("CONCURRENT-123"), code128.Options{}); encodeErr != nil {
				errors <- encodeErr
				return
			}
			input, renderErr := render.Image(shared.Logical(), render.Options{Scale: 3})
			if renderErr != nil {
				errors <- renderErr
				return
			}
			decoded, decodeErr := imagedecode.Decode(context.Background(), input, imagedecode.Options{
				Formats: []barcode.Format{barcode.QRCode},
			})
			if decodeErr != nil {
				errors <- decodeErr
				return
			}
			if string(decoded.Payload()) != "shared immutable symbol" {
				errors <- fmt.Errorf("decoded payload = %q", decoded.Payload())
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if got := shared.Logical().Matrix().Modules(); len(got) != len(wantModules) {
		t.Fatalf("shared matrix length changed from %d to %d", len(wantModules), len(got))
	} else {
		for index := range got {
			if got[index] != wantModules[index] {
				t.Fatalf("shared matrix changed at module %d", index)
			}
		}
	}
}
