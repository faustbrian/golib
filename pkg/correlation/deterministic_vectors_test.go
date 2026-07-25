package correlation_test

import (
	"encoding/json"
	"os"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

func TestDeterministicStrategyVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/deterministic_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Domain  string `json:"domain"`
		Version uint32 `json:"version"`
		Key     string `json:"key"`
		Input   string `json:"input"`
		Length  int    `json:"length"`
		Output  string `json:"output"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, vector := range vectors {
		strategy, err := correlation.NewDeterministic(correlation.DeterministicOptions{
			Domain: vector.Domain, Version: vector.Version,
			Key: []byte(vector.Key), Length: vector.Length,
		})
		if err != nil {
			t.Fatal(err)
		}
		got, err := strategy.Derive([]byte(vector.Input))
		if err != nil || got.String() != vector.Output {
			t.Fatalf("vector %s/v%d = %q, %v; want %q", vector.Domain, vector.Version, got, err, vector.Output)
		}
	}
}
