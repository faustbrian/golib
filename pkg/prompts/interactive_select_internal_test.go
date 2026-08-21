package prompts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestInteractiveSelectionContainsOwnedParserFailure(t *testing.T) {
	t.Parallel()

	details := selectionDetails{
		options: []selectionOption{{id: "one", label: "One"}},
		maximum: 1,
	}
	prompt := Prompt[string]{
		definition: definition[string]{
			kind: KindSelect,
			id: "choice",
			label: "Choice",
			retry: RetryPolicy{MaxAttempts: 1},
			selection: &details,
			parse: func(string) (string, error) {
				return "", errors.New("owned parser failed")
			},
		},
	}
	terminal := NewVirtualTerminal(40, 8)
	terminal.Push(KeyEvent(KeyEnter))
	_, err := Run(context.Background(), prompt, Execution{
		Output: terminal,
		Events: boundedInternalEventSource{
			EventSource: terminal,
			Wait:        100 * time.Millisecond,
		},
		Terminal:     terminal,
		Capabilities: Capabilities{InputTerminal: true, OutputTerminal: true},
		Policy:       InteractionPolicy{Mode: InteractiveRequired, PermitInteraction: true},
	})
	if !errors.Is(err, ErrValidationExhausted) || !terminal.Released() {
		t.Fatalf("Run() error = %v, released %v", err, terminal.Released())
	}
}

func TestSelectionStateEmptyAndDisabledOperationsAreStable(t *testing.T) {
	t.Parallel()

	empty := selectionState{details: selectionDetails{maximum: 1}, selected: map[string]bool{}}
	empty.ensureEnabled(1)
	empty.move(1, 1)
	empty.focusLast()
	empty.toggle()
	if input, ok := empty.submission();
		ok || input != "" || empty.message != "No selectable options" {
		t.Fatalf("empty submission = %q, %v, message %q", input, ok, empty.message)
	}

	disabled := selectionState{
		details: selectionDetails{
			options: []selectionOption{{id: "off", label: "Off", disabled: true}},
			maximum: 1,
		},
		visible: []int{0},
		selected: map[string]bool{},
	}
	disabled.ensureEnabled(1)
	disabled.toggle()
	if input, ok := disabled.submission(); ok || input != "" {
		t.Fatalf("disabled submission = %q, %v", input, ok)
	}
}

func TestSelectionStateFocusToggleAndRanking(t *testing.T) {
	t.Parallel()

	details := selectionDetails{
		options: []selectionOption{
			{id: "alpha", label: "Alpha", description: "first token"},
			{id: "beta", label: "Beta", description: "second match"},
		},
		initialIDs: []string{"alpha"},
		multiple: true,
		maximum: 2,
		searchPolicy: SearchPolicy{MaxOptions: 2, MaxResults: 1, MaxQueryRunes: 10},
	}
	state := newSelectionState(details, 20, 4)
	state.focusLast()
	state.toggle()
	state.toggle()
	state.focusFirst()
	state.toggle()
	if state.selected["alpha"] {
		t.Fatal("toggle did not remove an initial selection")
	}

	state.query = lineEditor{cells: splitGraphemes("beta"), cursor: 4, maxBytes: 40}
	state.filter()
	if len(state.visible) != 1 || state.details.options[state.visible[0]].id != "beta" {
		t.Fatalf("exact filter = %#v", state.visible)
	}
	state.query = lineEditor{cells: splitGraphemes("second ma"), cursor: 9, maxBytes: 40}
	state.filter()
	if len(state.visible) != 1 || state.details.options[state.visible[0]].id != "beta" {
		t.Fatalf("prefix-token filter = %#v", state.visible)
	}
	state.query = lineEditor{cells: splitGraphemes("cond at"), cursor: 7, maxBytes: 40}
	state.filter()
	if len(state.visible) != 1 || state.details.options[state.visible[0]].id != "beta" {
		t.Fatalf("contains-token filter = %#v", state.visible)
	}

	state.query = lineEditor{cells: splitGraphemes("a"), cursor: 1, maxBytes: 40}
	state.filter()
	if len(state.visible) != 1 {
		t.Fatalf("result limit = %#v", state.visible)
	}
	if !strings.Contains(state.details.options[state.visible[0]].label, "Alpha") {
		t.Fatalf("stable limited match = %#v", state.visible)
	}
}

func TestSelectionStateAppliesFormReplayDefensively(t *testing.T) {
	t.Parallel()

	details := selectionDetails{
		options: []selectionOption{
			{id: "disabled", label: "Disabled", disabled: true},
			{id: "active", label: "Active"},
		},
		multiple: true,
		maximum: 2,
		searchPolicy: SearchPolicy{MaxOptions: 2, MaxResults: 2, MaxQueryRunes: 10},
	}
	state := newSelectionState(details, 20, 4)
	state.applyReplay(
		selectionReplay{
			selected: []string{"missing", "disabled", "active"},
			focusID: "active",
			query: "act",
		},
	)
	if len(state.selected) != 1 ||
		!state.selected["active"] ||
		state.replay().focusID != "active" {
		t.Fatalf("replayed state = %#v, %#v", state.selected, state.replay())
	}
}

func TestInteractiveSelectionReplayAndExactEventBoundaries(t *testing.T) {
	t.Parallel()

	details := selectionDetails{
		options: []selectionOption{
			{id: "one", label: "One"},
			{id: "two", label: "Two"},
		},
		initialIDs: []string{"one"}, maximum: 1,
	}
	interaction := &formInteraction{initial: &formReplay{
		kind:      formReplaySelection,
		selection: selectionReplay{focusID: "two"},
	}}
	ctx := context.WithValue(context.Background(), formNavigationContextKey{}, interaction)
	value, err := runSelectionEvents(ctx, details, KeyEvent(KeyEnter))
	if err != nil || value != "two" {
		t.Fatalf("replayed selection = %q, %v", value, err)
	}

	value, err = runSelectionEvents(
		context.Background(),
		details,
		ResizeEvent(0, 0),
		KeyEvent(KeyEnter),
	)
	if err != nil || value != "one" {
		t.Fatalf("zero-size selection = %q, %v", value, err)
	}

	search := details
	search.searchPolicy = SearchPolicy{MaxOptions: 2, MaxResults: 2, MaxQueryRunes: 3}
	for name, events := range map[string][]InputEvent{
		"missing search policy": {PasteEvent(""), KeyEvent(KeyEnter)},
		"invalid UTF-8":         {PasteEvent(string([]byte{0xff})), KeyEvent(KeyEscape)},
		"paste overflow":        {PasteEvent("four"), KeyEvent(KeyEscape)},
		"rune overflow": {
			RuneEvent('a'), RuneEvent('b'), RuneEvent('c'), RuneEvent('d'),
			KeyEvent(KeyEscape),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			policy := search
			if name == "missing search policy" {
				policy = details
			}
			if _, runErr := runSelectionEvents(context.Background(), policy, events...); !errors.Is(runErr, ErrReader) {
				t.Fatalf("selection boundary error = %v", runErr)
			}
		})
	}
}

func TestInteractiveSelectionDistinguishesSearchFromMultiSelectToggle(t *testing.T) {
	t.Parallel()

	details := selectionDetails{
		options: []selectionOption{
			{id: "alpha", label: "Alpha"},
			{id: "beta", label: "Beta"},
		},
		initialIDs: []string{"alpha"}, multiple: true, maximum: 2,
		searchPolicy: SearchPolicy{MaxOptions: 2, MaxResults: 2, MaxQueryRunes: 4},
	}
	value, err := runSelectionEvents(
		context.Background(),
		details,
		RuneEvent('b'),
		RuneEvent(' '),
		KeyEvent(KeyEnter),
	)
	if err != nil || value != "alpha,beta" {
		t.Fatalf("searched multi-selection = %q, %v", value, err)
	}

	single := selectionDetails{
		options: []selectionOption{
			{id: "alpha", label: "Alpha"},
			{id: "beta", label: "Beta"},
			{id: "gamma", label: "Gamma"},
		},
		initialIDs: []string{"alpha"}, maximum: 1,
	}
	value, err = runSelectionEvents(
		context.Background(),
		single,
		KeyEvent(KeyShiftTab),
		KeyEvent(KeyEnter),
	)
	if err != nil || value != "gamma" {
		t.Fatalf("backward selection = %q, %v", value, err)
	}
}

func TestSelectionStateExactReplayAndNavigationBoundaries(t *testing.T) {
	t.Parallel()

	details := selectionDetails{
		options: []selectionOption{
			{id: "one", label: "One"},
			{id: "two", label: "Two"},
			{id: "three", label: "Three"},
		},
		initialIDs: []string{"two"}, maximum: 1,
	}
	state := newSelectionState(details, 20, 8)
	if state.focus != 1 {
		t.Fatalf("initial focus = %d, want 1", state.focus)
	}
	state.applyReplay(selectionReplay{focusID: "three"})
	if state.focus != 2 || state.replay().focusID != "three" {
		t.Fatalf("replayed focus = %d, %#v", state.focus, state.replay())
	}

	state.focus = -1
	if replay := state.replay(); replay.focusID != "" {
		t.Fatalf("negative-focus replay = %#v", replay)
	}
	state.focus = len(state.visible)
	if replay := state.replay(); replay.focusID != "" {
		t.Fatalf("end-focus replay = %#v", replay)
	}

	state.focus = 1
	state.move(1, 0)
	if state.focus != 1 {
		t.Fatalf("zero-distance focus = %d, want 1", state.focus)
	}
	state.move(-1, 1)
	if state.focus != 0 {
		t.Fatalf("backward focus = %d, want 0", state.focus)
	}

	disabledLast := newSelectionState(selectionDetails{
		options: []selectionOption{
			{id: "first", label: "First"},
			{id: "active", label: "Active"},
			{id: "disabled", label: "Disabled", disabled: true},
		},
		maximum: 1,
	}, 20, 8)
	disabledLast.focusLast()
	if disabledLast.focus != 1 {
		t.Fatalf("last enabled focus = %d, want 1", disabledLast.focus)
	}

	search := selectionDetails{
		options: []selectionOption{{id: "wide", label: "界"}}, maximum: 1,
		searchPolicy: SearchPolicy{MaxOptions: 1, MaxResults: 1, MaxQueryRunes: 1},
	}
	query := newSelectionState(search, 20, 8)
	query.applyReplay(selectionReplay{query: "界", focusID: "wide"})
	if query.query.text() != "界" || query.replay().focusID != "wide" {
		t.Fatalf("bounded query replay = %q, %#v", query.query.text(), query.replay())
	}
}

func TestSelectionPageSizeAndRenderingUseExactSemanticState(t *testing.T) {
	t.Parallel()

	state := selectionState{
		details:  selectionDetails{searchPolicy: SearchPolicy{MaxQueryRunes: 1}},
		height:   10,
		metadata: 2,
		message:  "invalid",
	}
	if size := state.pageSize(); size != 5 {
		t.Fatalf("page size = %d, want 5", size)
	}

	details := selectionDetails{maximum: 1, options: []selectionOption{
		{id: "a", label: "A"},
		{id: "b", label: "B"},
		{id: "c", label: "C"},
		{id: "d", label: "D", description: "fourth"},
		{id: "e", label: "E"},
	}}
	visible := []int{0, 1, 2, 3, 4}
	var frame Frame
	renderer := internalRendererFunc(func(value Frame, _ RenderOptions) (string, error) {
		frame = value
		return "", nil
	})
	err := writeSelection(
		Execution{Output: &strings.Builder{}, Renderer: renderer},
		definition[string]{id: "choice", label: "Choice"},
		selectionState{details: details, visible: visible, focus: 3, height: 3},
	)
	if err != nil {
		t.Fatal(err)
	}
	lines := frame.Lines()
	if len(lines) != 3 {
		t.Fatalf("rendered lines = %d, want 3", len(lines))
	}
	first := lines[1].Segments()
	second := lines[2].Segments()
	if len(first) != 1 || first[0].Content != "C" || first[0].Role != RoleValue {
		t.Fatalf("first visible option = %#v", first)
	}
	if len(second) != 4 || second[0].Role != RoleFocus ||
		second[1].Content != "D" || second[2].Content != " - " ||
		second[3].Role != RoleHint || second[3].Content != "fourth" {
		t.Fatalf("focused described option = %#v", second)
	}

	err = writeSelection(
		Execution{Output: &strings.Builder{}, Renderer: renderer},
		definition[string]{id: "choice", label: "Choice"},
		selectionState{details: details, visible: visible, focus: 2, height: 3},
	)
	if err != nil {
		t.Fatal(err)
	}
	lines = frame.Lines()
	first = lines[1].Segments()
	second = lines[2].Segments()
	if first[len(first)-1].Content != "C" || second[0].Content != "D" {
		t.Fatalf("exact-page-boundary options = %#v, %#v", first, second)
	}

	err = writeSelection(
		Execution{Output: &strings.Builder{}, Renderer: renderer},
		definition[string]{id: "choice", label: "Choice"},
		selectionState{
			details: selectionDetails{maximum: 1, options: []selectionOption{{
				id: "disabled", label: "Disabled", disabled: true,
			}}},
			visible: []int{0}, focus: 0, height: 3,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	disabled := frame.Lines()[1].Segments()
	if len(disabled) != 1 || disabled[0].Role != RoleDisabled {
		t.Fatalf("focused disabled option = %#v", disabled)
	}
}

func runSelectionEvents(
	ctx context.Context,
	details selectionDetails,
	events ...InputEvent,
) (string, error) {
	terminal := NewVirtualTerminal(40, 8)
	if err := terminal.Push(events...); err != nil {
		return "", err
	}
	prompt := Prompt[string]{definition: definition[string]{
		kind: KindSelect, id: "choice", label: "Choice",
		retry: RetryPolicy{MaxAttempts: 1}, selection: &details,
		parse: func(value string) (string, error) { return value, nil },
	}}

	return runInteractiveSelection(ctx, prompt, Execution{
		Output: terminal,
		Events: boundedInternalEventSource{
			EventSource: terminal,
			Wait:        100 * time.Millisecond,
		},
		Capabilities: Capabilities{
			InputTerminal: true, OutputTerminal: true, Width: 40, Height: 8,
		},
	}, details)
}

type internalRendererFunc func(Frame, RenderOptions) (string, error)

func (renderer internalRendererFunc) Render(frame Frame, options RenderOptions) (string, error) {
	return renderer(frame, options)
}
