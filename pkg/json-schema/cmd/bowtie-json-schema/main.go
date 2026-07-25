// Command bowtie-json-schema implements the Bowtie harness protocol.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

var supportedDialects = []jsonschema.Dialect{
	jsonschema.Draft3,
	jsonschema.Draft4,
	jsonschema.Draft6,
	jsonschema.Draft7,
	jsonschema.Draft201909,
	jsonschema.Draft202012,
}

type harness struct {
	started         bool
	dialect         jsonschema.Dialect
	compilerFactory func(...jsonschema.Option) (*jsonschema.Compiler, error)
}

type commandEnvelope struct {
	Command string `json:"cmd"`
}

type dialectCommand struct {
	Dialect string `json:"dialect"`
}

type runCommand struct {
	Sequence json.RawMessage `json:"seq"`
	Case     struct {
		Schema   json.RawMessage            `json:"schema"`
		Registry map[string]json.RawMessage `json:"registry"`
		Tests    []struct {
			Instance json.RawMessage `json:"instance"`
		} `json:"tests"`
	} `json:"case"`
}

type runResponse struct {
	Sequence json.RawMessage `json:"seq"`
	Results  []caseResult    `json:"results,omitempty"`
	Errored  bool            `json:"errored,omitempty"`
	Context  map[string]any  `json:"context,omitempty"`
}

type caseResult struct {
	Valid bool `json:"valid"`
}

var (
	serveHarness                 = serve
	harnessInput       io.Reader = os.Stdin
	harnessOutput      io.Writer = os.Stdout
	harnessErrorOutput io.Writer = os.Stderr
	exitProcess                  = os.Exit
)

func main() {
	if err := serveHarness(context.Background(), harnessInput, harnessOutput); err != nil {
		_, _ = fmt.Fprintln(harnessErrorOutput, err)
		exitProcess(1)
	}
}

func serve(ctx context.Context, input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(input)
	decoder.UseNumber()
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	state := harness{dialect: jsonschema.Draft202012}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("decode Bowtie command: %w", err)
		}
		var envelope commandEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("decode Bowtie command envelope: %w", err)
		}
		response, stop, err := state.handle(ctx, envelope.Command, raw)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
		if err := encoder.Encode(response); err != nil {
			return fmt.Errorf("encode Bowtie response: %w", err)
		}
	}
}

func (state *harness) handle(
	ctx context.Context,
	command string,
	raw []byte,
) (any, bool, error) {
	switch command {
	case "start":
		var request struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(raw, &request); err != nil || request.Version != 1 {
			return nil, false, fmt.Errorf("unsupported Bowtie protocol version")
		}
		state.started = true
		dialects := make([]string, len(supportedDialects))
		for index, dialect := range supportedDialects {
			dialects[index] = string(dialect)
		}
		return map[string]any{
			"version": 1,
			"implementation": map[string]any{
				"language": "go",
				"name":     "json-schema",
				"homepage": "https://github.com/faustbrian/golib",
				"issues":   "https://github.com/faustbrian/golib/issues",
				"source":   "https://github.com/faustbrian/golib/tree/main/json-schema",
				"dialects": dialects,
			},
		}, false, nil
	case "dialect":
		if !state.started {
			return nil, false, fmt.Errorf("bowtie harness has not started")
		}
		var request dialectCommand
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, false, err
		}
		dialect, supported := releasedDialect(request.Dialect)
		if !supported {
			return map[string]bool{"ok": false}, false, nil
		}
		state.dialect = dialect
		return map[string]bool{"ok": true}, false, nil
	case "run":
		if !state.started {
			return nil, false, fmt.Errorf("bowtie harness has not started")
		}
		var request runCommand
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, false, err
		}
		return state.run(ctx, request), false, nil
	case "stop":
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("unknown Bowtie command %q", command)
	}
}

func (state *harness) run(ctx context.Context, request runCommand) (response runResponse) {
	response.Sequence = append(json.RawMessage(nil), request.Sequence...)
	defer func() {
		if recovered := recover(); recovered != nil {
			response.Results = nil
			response.Errored = true
			response.Context = map[string]any{"message": fmt.Sprint(recovered)}
		}
	}()
	resources := make(map[string][]byte, len(request.Case.Registry))
	for identifier, raw := range request.Case.Registry {
		resources[identifier] = append([]byte(nil), raw...)
	}
	loader, err := jsonschema.NewMapLoader(resources)
	if err != nil {
		return caughtError(response, err)
	}
	compilerFactory := state.compilerFactory
	if compilerFactory == nil {
		compilerFactory = jsonschema.NewCompiler
	}
	compiler, err := compilerFactory(
		jsonschema.WithDialect(state.dialect),
		jsonschema.WithResourceLoader(loader),
	)
	if err != nil {
		return caughtError(response, err)
	}
	schema, err := compiler.Compile(ctx, request.Case.Schema)
	if err != nil {
		return caughtError(response, err)
	}
	response.Results = make([]caseResult, 0, len(request.Case.Tests))
	for _, test := range request.Case.Tests {
		result, err := schema.Validate(ctx, test.Instance)
		if err != nil {
			return caughtError(response, err)
		}
		response.Results = append(response.Results, caseResult{Valid: result.Valid})
	}
	return response
}

func caughtError(response runResponse, err error) runResponse {
	response.Results = nil
	response.Errored = true
	response.Context = map[string]any{"message": err.Error()}
	return response
}

func releasedDialect(identifier string) (jsonschema.Dialect, bool) {
	for _, dialect := range supportedDialects {
		if identifier == string(dialect) {
			return dialect, true
		}
	}
	return "", false
}
