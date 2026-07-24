package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunRejectsInvalidComparisonDelay(t *testing.T) {
	for _, value := range []string{"invalid", "60001"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("COMPARE_DELAY_MS", value)

			err := run()
			if err == nil || !strings.Contains(err.Error(), "0 through 60000") {
				t.Fatalf("run() error = %v, want bounded delay error", err)
			}
		})
	}
}

func TestRunEmitsVerifiedCanonicalPlan(t *testing.T) {
	t.Setenv("COMPARE_DELAY_MS", "0")

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = original })

	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}

	var output adapterOutput
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if output.AdapterSchema != "v2" || len(output.Containers) != 1 {
		t.Fatalf("run() output = %+v", output)
	}
	container := output.Containers[0]
	if container.ID != "box#000001" || container.TypeID != "box" ||
		len(container.Placements) != 2 {
		t.Fatalf("run() container = %+v", container)
	}
	for _, placement := range container.Placements {
		if placement.ItemID == "" || placement.Width != 2 ||
			placement.Length != 1 || placement.Depth != 1 ||
			placement.Weight != 1 {
			t.Fatalf("run() placement = %+v", placement)
		}
	}
}

func TestComparisonRequestIsValid(t *testing.T) {
	request, err := comparisonRequest()
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Items()) != 2 || len(request.Containers()) != 1 {
		t.Fatalf("comparisonRequest() returned unexpected fixture: %+v", request)
	}
}
