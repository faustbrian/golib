package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestServeImplementsBowtieProtocol(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		`{"cmd":"start","version":1}`,
		`{"cmd":"dialect","dialect":"https://json-schema.org/draft/2020-12/schema"}`,
		`{"cmd":"run","seq":{"case":1},"case":{"schema":{"$ref":"https://schemas.example.test/value"},"registry":{"https://schemas.example.test/value":{"type":"integer"}},"tests":[{"instance":2},{"instance":"no"}]}}`,
		`{"cmd":"stop"}`,
	}, "\n"))
	var output bytes.Buffer
	if err := serve(context.Background(), input, &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var started struct {
		Version        int `json:"version"`
		Implementation struct {
			Dialects []string `json:"dialects"`
		} `json:"implementation"`
	}
	if err := decoder.Decode(&started); err != nil {
		t.Fatal(err)
	}
	if started.Version != 1 || len(started.Implementation.Dialects) != 6 {
		t.Fatalf("unexpected start response: %#v", started)
	}
	var selected struct {
		OK bool `json:"ok"`
	}
	if err := decoder.Decode(&selected); err != nil {
		t.Fatal(err)
	}
	if !selected.OK {
		t.Fatal("dialect was not acknowledged")
	}
	var run struct {
		Sequence json.RawMessage `json:"seq"`
		Results  []struct {
			Valid bool `json:"valid"`
		} `json:"results"`
	}
	if err := decoder.Decode(&run); err != nil {
		t.Fatal(err)
	}
	if string(run.Sequence) != `{"case":1}` || len(run.Results) != 2 ||
		!run.Results[0].Valid || run.Results[1].Valid {
		t.Fatalf("unexpected run response: %#v", run)
	}
}

func TestHarnessRejectsInvalidProtocolSequences(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		command string
		raw     string
	}{
		{name: "unsupported start", command: "start", raw: `{"cmd":"start","version":2}`},
		{name: "malformed start", command: "start", raw: `{"cmd":"start","version":`},
		{name: "dialect before start", command: "dialect", raw: `{"cmd":"dialect"}`},
		{name: "run before start", command: "run", raw: `{"cmd":"run"}`},
		{name: "unknown command", command: "unknown", raw: `{"cmd":"unknown"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := harness{}
			if _, _, err := state.handle(
				context.Background(), test.command, []byte(test.raw),
			); err == nil {
				t.Fatal("expected protocol error")
			}
		})
	}
}

func TestHarnessAcknowledgesUnsupportedDialectAndStop(t *testing.T) {
	t.Parallel()

	state := harness{started: true}
	response, stop, err := state.handle(
		context.Background(),
		"dialect",
		[]byte(`{"dialect":"https://schemas.example.test/unknown"}`),
	)
	if err != nil || stop || response.(map[string]bool)["ok"] {
		t.Fatalf("unexpected dialect response %#v, %t, %v", response, stop, err)
	}
	response, stop, err = state.handle(context.Background(), "stop", []byte(`{}`))
	if err != nil || !stop || response != nil {
		t.Fatalf("unexpected stop response %#v, %t, %v", response, stop, err)
	}
}

func TestServeReportsInputContextAndOutputFailures(t *testing.T) {
	t.Parallel()

	if err := serve(context.Background(), strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("serve EOF: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serve(canceled, strings.NewReader(""), io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context cancellation", err)
	}
	for _, input := range []string{`{`, `[]`} {
		if err := serve(context.Background(), strings.NewReader(input), io.Discard); err == nil {
			t.Fatalf("%q: expected decode or command error", input)
		}
	}
	if err := serve(
		context.Background(),
		strings.NewReader(`{"cmd":"unknown"}`),
		io.Discard,
	); err == nil {
		t.Fatal("expected command handling error")
	}
	if err := serve(
		context.Background(),
		strings.NewReader(`{"cmd":"start","version":1}`),
		failingWriter{},
	); !errors.Is(err, errWrite) {
		t.Fatalf("got %v, want writer error", err)
	}
}

func TestHarnessRejectsMalformedDialectAndRunCommands(t *testing.T) {
	t.Parallel()

	state := harness{started: true}
	for _, command := range []string{"dialect", "run"} {
		if _, _, err := state.handle(
			context.Background(), command, []byte(`{"cmd":`),
		); err == nil {
			t.Fatalf("%s: expected malformed command error", command)
		}
	}
}

func TestHarnessRunCatchesCompilerFailuresAndPanics(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("compiler construction failed")
	request := runCommand{Sequence: json.RawMessage(`17`)}
	request.Case.Schema = json.RawMessage(`true`)
	state := harness{
		dialect: jsonschema.Draft202012,
		compilerFactory: func(...jsonschema.Option) (*jsonschema.Compiler, error) {
			return nil, sentinel
		},
	}
	response := state.run(context.Background(), request)
	if !response.Errored || !strings.Contains(response.Context["message"].(string), sentinel.Error()) {
		t.Fatalf("unexpected compiler failure response: %#v", response)
	}

	state.compilerFactory = func(...jsonschema.Option) (*jsonschema.Compiler, error) {
		panic("compiler panic")
	}
	response = state.run(context.Background(), request)
	if !response.Errored || response.Context["message"] != "compiler panic" {
		t.Fatalf("unexpected panic response: %#v", response)
	}
}

func TestMainReportsServeFailure(t *testing.T) {
	previousServe := serveHarness
	previousErrorOutput := harnessErrorOutput
	previousExit := exitProcess
	t.Cleanup(func() {
		serveHarness = previousServe
		harnessErrorOutput = previousErrorOutput
		exitProcess = previousExit
	})

	sentinel := errors.New("serve failed")
	serveHarness = func(context.Context, io.Reader, io.Writer) error {
		return sentinel
	}
	var stderr bytes.Buffer
	harnessErrorOutput = &stderr
	exitCode := 0
	exitProcess = func(code int) { exitCode = code }

	main()
	if exitCode != 1 || !strings.Contains(stderr.String(), sentinel.Error()) {
		t.Fatalf("got exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestHarnessRunCatchesRegistryAndInstanceErrors(t *testing.T) {
	t.Parallel()

	state := harness{started: true, dialect: jsonschema.Draft7}
	request := runCommand{Sequence: json.RawMessage(`9`)}
	request.Case.Registry = map[string]json.RawMessage{"": json.RawMessage(`{}`)}
	response := state.run(context.Background(), request)
	if !response.Errored || response.Context["message"] == "" {
		t.Fatalf("unexpected registry response %#v", response)
	}

	request.Case.Registry = nil
	request.Case.Schema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema"}`)
	request.Case.Tests = append(request.Case.Tests, struct {
		Instance json.RawMessage `json:"instance"`
	}{Instance: json.RawMessage(`{`)})
	response = state.run(context.Background(), request)
	if !response.Errored {
		t.Fatalf("unexpected instance response %#v", response)
	}
}

var errWrite = errors.New("write failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errWrite
}

func TestServeReportsCaughtCompilationErrors(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		`{"cmd":"start","version":1}`,
		`{"cmd":"dialect","dialect":"http://json-schema.org/draft-07/schema#"}`,
		`{"cmd":"run","seq":7,"case":{"schema":{"type":7},"tests":[{"instance":2}]}}`,
		`{"cmd":"stop"}`,
	}, "\n"))
	var output bytes.Buffer
	if err := serve(context.Background(), input, &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var ignored any
	if err := decoder.Decode(&ignored); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&ignored); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Sequence int  `json:"seq"`
		Errored  bool `json:"errored"`
	}
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Sequence != 7 || !response.Errored {
		t.Fatalf("unexpected error response: %#v", response)
	}
}
