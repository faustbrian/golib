package eventsourcing_test

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestUpcasterChainRenamesSplitsAndAdvancesInOrder(t *testing.T) {
	t.Parallel()

	rename := mustUpcastRule(
		t,
		"legacy.user-created",
		1,
		func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			metadata := input.Metadata()
			metadata["migrated"] = "true"

			return []eventsourcing.UpcastEvent{
				mustUpcastEvent(t, "user.created", 1, input.Event().Payload(), metadata),
			}, nil
		},
	)
	split := mustUpcastRule(
		t,
		"user.created",
		1,
		func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			return []eventsourcing.UpcastEvent{
				mustUpcastEvent(t, "user.registered", 1, []byte(`{"id":42}`), input.Metadata()),
				mustUpcastEvent(t, "user.email-changed", 1, []byte(`{"email":"a@example.com"}`), input.Metadata()),
			}, nil
		},
	)
	advance := mustUpcastRule(
		t,
		"user.email-changed",
		1,
		func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			return []eventsourcing.UpcastEvent{
				mustUpcastEvent(t, "user.email-changed", 2, input.Event().Payload(), input.Metadata()),
			}, nil
		},
	)
	chain, err := eventsourcing.NewUpcasterChain(rename, split, advance)
	if err != nil {
		t.Fatal(err)
	}
	input := mustUpcastEvent(
		t,
		"legacy.user-created",
		1,
		readGoldenPayload(t, "testdata/upcast/legacy-user-created-v1.json"),
		map[string]string{"source": "legacy"},
	)

	output, err := chain.Upcast(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 2 {
		t.Fatalf("Upcast() length = %d, want 2", len(output))
	}
	if output[0].Event().Name().String() != "user.registered" ||
		output[0].Event().Version() != 1 ||
		output[1].Event().Name().String() != "user.email-changed" ||
		output[1].Event().Version() != 2 {
		t.Fatalf("Upcast() identities = %s/%d, %s/%d",
			output[0].Event().Name().String(),
			output[0].Event().Version(),
			output[1].Event().Name().String(),
			output[1].Event().Version(),
		)
	}
	for _, event := range output {
		if event.Metadata()["source"] != "legacy" ||
			event.Metadata()["migrated"] != "true" {
			t.Fatalf("metadata = %v", event.Metadata())
		}
	}
	for index, path := range []string{
		"testdata/upcast/user-registered-v1.json",
		"testdata/upcast/user-email-changed-v2.json",
	} {
		if !bytes.Equal(output[index].Event().Payload(), readGoldenPayload(t, path)) {
			t.Fatalf("upcast output %d differs from its golden fixture", index)
		}
	}

	payload := output[0].Event().Payload()
	payload[0] = '!'
	metadata := output[0].Metadata()
	metadata["source"] = "mutated"
	if output[0].Event().Payload()[0] == '!' ||
		output[0].Metadata()["source"] != "legacy" {
		t.Fatal("upcast output aliases caller-owned data")
	}
}

func readGoldenPayload(t *testing.T, path string) []byte {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden payload: %v", err)
	}

	return bytes.TrimSuffix(payload, []byte{'\n'})
}

func TestUpcasterChainReturnsUnmatchedEventDefensively(t *testing.T) {
	t.Parallel()

	chain, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	input := mustUpcastEvent(
		t,
		"account.opened",
		1,
		[]byte(`{"id":42}`),
		map[string]string{"source": "api"},
	)
	output, err := chain.Upcast(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 1 ||
		output[0].Event().Name() != input.Event().Name() ||
		output[0].Event().Version() != input.Event().Version() {
		t.Fatalf("Upcast() = %#v", output)
	}
}

func TestUpcasterChainRejectsCyclesNonProgressAndRegression(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		rules []eventsourcing.UpcastRule
		input eventsourcing.UpcastEvent
		want  error
	}{
		"same identity": {
			rules: []eventsourcing.UpcastRule{
				mustUpcastRule(t, "event.a", 1, replaceUpcast(t, "event.a", 1)),
			},
			input: mustUpcastEvent(t, "event.a", 1, []byte("{}"), nil),
			want:  eventsourcing.ErrUpcastNonProgress,
		},
		"schema regression": {
			rules: []eventsourcing.UpcastRule{
				mustUpcastRule(t, "event.a", 2, replaceUpcast(t, "event.a", 1)),
			},
			input: mustUpcastEvent(t, "event.a", 2, []byte("{}"), nil),
			want:  eventsourcing.ErrUpcastNonProgress,
		},
		"rename cycle": {
			rules: []eventsourcing.UpcastRule{
				mustUpcastRule(t, "event.a", 1, replaceUpcast(t, "event.b", 1)),
				mustUpcastRule(t, "event.b", 1, replaceUpcast(t, "event.a", 1)),
			},
			input: mustUpcastEvent(t, "event.a", 1, []byte("{}"), nil),
			want:  eventsourcing.ErrUpcastCycle,
		},
	}

	for name, testCase := range cases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			chain, err := eventsourcing.NewUpcasterChain(testCase.rules...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := chain.Upcast(testCase.input); !errors.Is(err, testCase.want) {
				t.Fatalf("Upcast() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestUpcasterChainRequiresDeterministicOutput(t *testing.T) {
	t.Parallel()

	call := 0
	rule := mustUpcastRule(
		t,
		"event.a",
		1,
		func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			call++

			return []eventsourcing.UpcastEvent{
				mustUpcastEvent(t, "event.b", 1, []byte(strconv.Itoa(call)), input.Metadata()),
			}, nil
		},
	)
	chain, err := eventsourcing.NewUpcasterChain(rule)
	if err != nil {
		t.Fatal(err)
	}

	_, err = chain.Upcast(mustUpcastEvent(t, "event.a", 1, []byte("{}"), nil))
	if !errors.Is(err, eventsourcing.ErrNonDeterministicUpcast) {
		t.Fatalf("Upcast() error = %v", err)
	}
}

func TestUpcasterChainRejectsNonDeterministicOutputLength(t *testing.T) {
	t.Parallel()

	call := 0
	rule := mustUpcastRule(
		t,
		"event.a",
		1,
		func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			call++
			output := []eventsourcing.UpcastEvent{
				mustUpcastEvent(t, "event.b", 1, input.Event().Payload(), nil),
			}
			if call%2 == 0 {
				output = append(
					output,
					mustUpcastEvent(t, "event.c", 1, input.Event().Payload(), nil),
				)
			}

			return output, nil
		},
	)
	chain, err := eventsourcing.NewUpcasterChain(rule)
	if err != nil {
		t.Fatal(err)
	}

	_, err = chain.Upcast(mustUpcastEvent(t, "event.a", 1, []byte("{}"), nil))
	if !errors.Is(err, eventsourcing.ErrNonDeterministicUpcast) {
		t.Fatalf("Upcast() error = %v", err)
	}
}

func TestUpcasterChainContainsCallbackFailureAndPanic(t *testing.T) {
	t.Parallel()

	secret := errors.New("credential-secret")
	cases := map[string]struct {
		upcaster eventsourcing.UpcasterFunc
		want     error
	}{
		"error": {
			upcaster: func(eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
				return nil, secret
			},
			want: secret,
		},
		"panic": {
			upcaster: func(eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
				panic("private-panic-value")
			},
			want: eventsourcing.ErrUpcasterPanic,
		},
		"invalid output": {
			upcaster: func(eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
				return []eventsourcing.UpcastEvent{{}}, nil
			},
			want: eventsourcing.ErrInvalidArgument,
		},
	}

	for name, testCase := range cases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rule := mustUpcastRule(t, "event.a", 1, testCase.upcaster)
			chain, err := eventsourcing.NewUpcasterChain(rule)
			if err != nil {
				t.Fatal(err)
			}
			_, err = chain.Upcast(mustUpcastEvent(t, "event.a", 1, []byte("{}"), nil))
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Upcast() error = %v, want %v", err, testCase.want)
			}
			if strings.Contains(err.Error(), "credential-secret") ||
				strings.Contains(err.Error(), "private-panic-value") {
				t.Fatalf("Upcast() disclosed callback diagnostic: %q", err)
			}
			var upcastErr *eventsourcing.UpcastError
			if !errors.As(err, &upcastErr) ||
				upcastErr.EventName.String() != "event.a" ||
				upcastErr.SchemaVersion != 1 {
				t.Fatalf("UpcastError = %#v", upcastErr)
			}
		})
	}
}

func TestUpcasterChainRequiresReviewedDropPolicy(t *testing.T) {
	t.Parallel()

	drop := func(eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
		return nil, nil
	}
	unreviewed := mustUpcastRule(t, "event.obsolete", 1, drop)
	chain, err := eventsourcing.NewUpcasterChain(unreviewed)
	if err != nil {
		t.Fatal(err)
	}
	input := mustUpcastEvent(t, "event.obsolete", 1, []byte("{}"), nil)
	if _, err := chain.Upcast(input); !errors.Is(
		err,
		eventsourcing.ErrUpcastDropNotAllowed,
	) {
		t.Fatalf("Upcast(unreviewed drop) error = %v", err)
	}

	policy, err := eventsourcing.NewReviewedDropPolicy(
		"event is superseded by authoritative import",
		"maintainer@example.com",
		time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Rationale() != "event is superseded by authoritative import" ||
		policy.Reviewer() != "maintainer@example.com" ||
		!policy.ReviewedAt().Equal(
			time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC),
		) {
		t.Fatalf("drop policy accessors returned unexpected values")
	}
	reviewed := mustUpcastRule(
		t,
		"event.obsolete",
		1,
		drop,
		eventsourcing.AllowUpcastDrop(policy),
	)
	chain, err = eventsourcing.NewUpcasterChain(reviewed)
	if err != nil {
		t.Fatal(err)
	}
	output, err := chain.Upcast(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 0 {
		t.Fatalf("Upcast(reviewed drop) length = %d", len(output))
	}
}

func TestUpcasterChainEnforcesOutputAndStepBounds(t *testing.T) {
	t.Parallel()

	tooMany := mustUpcastRule(
		t,
		"event.a",
		1,
		func(eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			output := make([]eventsourcing.UpcastEvent, eventsourcing.MaxUpcastSegments+1)
			for index := range output {
				output[index] = mustUpcastEvent(
					t,
					"event.output-"+strconv.Itoa(index),
					1,
					[]byte("{}"),
					nil,
				)
			}

			return output, nil
		},
	)
	chain, err := eventsourcing.NewUpcasterChain(tooMany)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chain.Upcast(
		mustUpcastEvent(t, "event.a", 1, []byte("{}"), nil),
	); !errors.Is(err, eventsourcing.ErrUpcastLimit) {
		t.Fatalf("Upcast(output limit) error = %v", err)
	}

	rules := make([]eventsourcing.UpcastRule, eventsourcing.MaxUpcastSteps+1)
	for index := range rules {
		source := "event.n" + strconv.Itoa(index)
		target := "event.n" + strconv.Itoa(index+1)
		rules[index] = mustUpcastRule(t, source, 1, replaceUpcast(t, target, 1))
	}
	chain, err = eventsourcing.NewUpcasterChain(rules...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chain.Upcast(
		mustUpcastEvent(t, "event.n0", 1, []byte("{}"), nil),
	); !errors.Is(err, eventsourcing.ErrUpcastLimit) {
		t.Fatalf("Upcast(step limit) error = %v", err)
	}

	firstLevel := mustUpcastRule(
		t,
		"event.root",
		1,
		repeatUpcast(t, "event.branch", eventsourcing.MaxUpcastSegments),
	)
	secondLevel := mustUpcastRule(
		t,
		"event.branch",
		1,
		repeatUpcast(t, "event.leaf", 2),
	)
	chain, err = eventsourcing.NewUpcasterChain(firstLevel, secondLevel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chain.Upcast(
		mustUpcastEvent(t, "event.root", 1, []byte("{}"), nil),
	); !errors.Is(err, eventsourcing.ErrUpcastLimit) {
		t.Fatalf("Upcast(aggregate output limit) error = %v", err)
	}

	policy, err := eventsourcing.NewReviewedDropPolicy(
		"bounded work regression",
		"reviewer",
		time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	secondLevel = mustUpcastRule(
		t,
		"event.branch",
		1,
		repeatUpcast(t, "event.leaf", 4),
	)
	dropLeaves := mustUpcastRule(
		t,
		"event.leaf",
		1,
		func(eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			return nil, nil
		},
		eventsourcing.AllowUpcastDrop(policy),
	)
	chain, err = eventsourcing.NewUpcasterChain(
		firstLevel,
		secondLevel,
		dropLeaves,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chain.Upcast(
		mustUpcastEvent(t, "event.root", 1, []byte("{}"), nil),
	); !errors.Is(err, eventsourcing.ErrUpcastLimit) {
		t.Fatalf("Upcast(work limit) error = %v", err)
	}
}

func TestUpcasterConstructionValidation(t *testing.T) {
	t.Parallel()

	if _, err := eventsourcing.NewUpcastEvent(eventsourcing.EncodedEvent{}, nil); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewUpcastEvent(zero) error = %v", err)
	}
	event := mustUpcastEvent(t, "event.a", 1, []byte("{}"), nil).Event()
	if _, err := eventsourcing.NewUpcastEvent(
		event,
		map[string]string{"es.reserved": "value"},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewUpcastEvent(metadata) error = %v", err)
	}
	if _, err := eventsourcing.NewUpcastRule(
		"Invalid Event",
		1,
		replaceUpcast(t, "event.b", 1),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewUpcastRule(name) error = %v", err)
	}
	if _, err := eventsourcing.NewUpcastRule(
		"event.a",
		0,
		replaceUpcast(t, "event.b", 1),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewUpcastRule(version) error = %v", err)
	}
	if _, err := eventsourcing.NewUpcastRule("event.a", 1, nil); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewUpcastRule(nil) error = %v", err)
	}
	var nilOption eventsourcing.UpcastRuleOption
	if _, err := eventsourcing.NewUpcastRule(
		"event.a",
		1,
		replaceUpcast(t, "event.b", 1),
		nilOption,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewUpcastRule(nil option) error = %v", err)
	}
	validPolicy, err := eventsourcing.NewReviewedDropPolicy(
		"reviewed reason",
		"reviewer",
		time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eventsourcing.NewUpcastRule(
		"event.a",
		1,
		replaceUpcast(t, "event.b", 1),
		eventsourcing.AllowUpcastDrop(eventsourcing.ReviewedDropPolicy{}),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewUpcastRule(zero drop policy) error = %v", err)
	}
	if _, err := eventsourcing.NewUpcastRule(
		"event.a",
		1,
		replaceUpcast(t, "event.b", 1),
		eventsourcing.AllowUpcastDrop(validPolicy),
		eventsourcing.AllowUpcastDrop(validPolicy),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewUpcastRule(duplicate drop policy) error = %v", err)
	}

	rule := mustUpcastRule(t, "event.a", 1, replaceUpcast(t, "event.b", 1))
	if _, err := eventsourcing.NewUpcasterChain(rule, rule); !errors.Is(
		err,
		eventsourcing.ErrDuplicateRegistration,
	) {
		t.Fatalf("NewUpcasterChain(duplicate) error = %v", err)
	}
	if _, err := eventsourcing.NewUpcasterChain(eventsourcing.UpcastRule{}); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewUpcasterChain(zero rule) error = %v", err)
	}
	chain, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chain.Upcast(eventsourcing.UpcastEvent{}); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("Upcast(zero) error = %v", err)
	}
	var nilChain *eventsourcing.UpcasterChain
	if _, err := nilChain.Upcast(
		mustUpcastEvent(t, "event.a", 1, []byte("{}"), nil),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil Upcast() error = %v", err)
	}

	for name, input := range map[string]struct {
		rationale string
		reviewer  string
		at        time.Time
	}{
		"rationale": {reviewer: "reviewer", at: time.Now()},
		"reviewer":  {rationale: "reason", at: time.Now()},
		"time":      {rationale: "reason", reviewer: "reviewer"},
	} {
		input := input
		t.Run("drop policy "+name, func(t *testing.T) {
			t.Parallel()

			if _, err := eventsourcing.NewReviewedDropPolicy(
				input.rationale,
				input.reviewer,
				input.at,
			); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("NewReviewedDropPolicy() error = %v", err)
			}
		})
	}
}

func mustUpcastEvent(
	t *testing.T,
	name string,
	version eventsourcing.SchemaVersion,
	payload []byte,
	metadata map[string]string,
) eventsourcing.UpcastEvent {
	t.Helper()

	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        name,
		Version:     version,
		ContentType: eventsourcing.JSONContentType,
		Payload:     payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	input, err := eventsourcing.NewUpcastEvent(event, metadata)
	if err != nil {
		t.Fatal(err)
	}

	return input
}

func mustUpcastRule(
	t *testing.T,
	name string,
	version eventsourcing.SchemaVersion,
	upcaster eventsourcing.UpcasterFunc,
	options ...eventsourcing.UpcastRuleOption,
) eventsourcing.UpcastRule {
	t.Helper()

	rule, err := eventsourcing.NewUpcastRule(name, version, upcaster, options...)
	if err != nil {
		t.Fatal(err)
	}

	return rule
}

func replaceUpcast(
	t *testing.T,
	name string,
	version eventsourcing.SchemaVersion,
) eventsourcing.UpcasterFunc {
	t.Helper()

	return func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
		return []eventsourcing.UpcastEvent{
			mustUpcastEvent(t, name, version, input.Event().Payload(), input.Metadata()),
		}, nil
	}
}

func repeatUpcast(
	t *testing.T,
	name string,
	count int,
) eventsourcing.UpcasterFunc {
	t.Helper()

	return func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
		output := make([]eventsourcing.UpcastEvent, count)
		for index := range output {
			output[index] = mustUpcastEvent(
				t,
				name,
				1,
				input.Event().Payload(),
				input.Metadata(),
			)
		}

		return output, nil
	}
}
