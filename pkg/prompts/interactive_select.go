package prompts

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

type selectionState struct {
	details  selectionDetails
	visible  []int
	focus    int
	selected map[string]bool
	query    lineEditor
	message  string
	width    int
	height   int
	metadata int
}

func runInteractiveSelection[T any](ctx context.Context, prompt Prompt[T], execution Execution, details selectionDetails) (T, error) {
	state := newSelectionState(details, execution.Capabilities.Width, execution.Capabilities.Height)
	navigation := formInteractionFrom(ctx)
	if navigation != nil && navigation.initial != nil && navigation.initial.kind == formReplaySelection {
		state.applyReplay(navigation.initial.selection)
	}
	state.metadata = len(presentationMetadata(prompt.definition))
	if err := writeSelection(execution, prompt.definition, state); err != nil {
		var zero T
		return zero, err
	}
	attempts := uint(0)
	for {
		event, err := execution.Events.Next(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				var zero T
				return zero, contextFailure(prompt.ID(), ctxErr)
			}
			if errors.Is(err, io.EOF) {
				return resolveEOF(ctx, prompt, execution.Dependencies)
			}
			var zero T
			return zero, eventReadFailure(prompt.ID(), "read selection event", err)
		}
		submit := false
		switch event.Kind {
		case EventEOF:
			return resolveEOF(ctx, prompt, execution.Dependencies)
		case EventDetached:
			var zero T
			return zero, streamFailure(prompt.ID(), ErrorTerminalDetached, "read selection event", ErrTerminalDetached)
		case EventResize:
			if event.Width < 0 || event.Height < 0 {
				var zero T
				return zero, streamFailure(prompt.ID(), ErrorReader, "resize selection", ErrReader)
			}
			state.width, state.height = event.Width, event.Height
		case EventCapabilities:
			if err := applyCapabilityChange(
				&execution, event.Capabilities, &state.width, &state.height,
			); err != nil {
				var zero T
				if errors.Is(err, ErrTerminalDetached) {
					return zero, streamFailure(prompt.ID(), ErrorTerminalDetached, "update selection capabilities", err)
				}
				return zero, streamFailure(prompt.ID(), ErrorReader, "update selection capabilities", err)
			}
		case EventPaste:
			if details.searchPolicy == (SearchPolicy{}) || !utf8.ValidString(event.Text) ||
				utf8.RuneCountInString(state.query.text())+utf8.RuneCountInString(event.Text) > details.searchPolicy.MaxQueryRunes {
				var zero T
				return zero, streamFailure(prompt.ID(), ErrorReader, "search selection", ErrReader)
			}
			if err := state.query.insert(event.Text, false); err != nil {
				var zero T
				return zero, streamFailure(prompt.ID(), ErrorReader, "search selection", err)
			}
			state.filter()
		case EventKey:
			event = execution.Keys.translate(event)
			if navigation != nil && event.Key == KeyShiftTab {
				navigation.captureSelection(state.replay())
				var zero T
				return zero, errFormBack
			}
			switch event.Key {
			case KeyEscape, KeyCtrlC:
				return resolveCancel(ctx, prompt, execution.Dependencies)
			case KeyCtrlD:
				return resolveEOF(ctx, prompt, execution.Dependencies)
			case KeyEnter:
				submit = true
			case KeyUp:
				state.move(-1)
			case KeyDown:
				state.move(1)
			case KeyPageUp:
				state.move(-state.pageSize())
			case KeyPageDown:
				state.move(state.pageSize())
			case KeyHome:
				state.focusFirst()
			case KeyEnd:
				state.focusLast()
			case KeyRune:
				if details.multiple && event.Rune == ' ' {
					state.toggle()
				} else if details.searchPolicy != (SearchPolicy{}) {
					if utf8.RuneCountInString(state.query.text()) >= details.searchPolicy.MaxQueryRunes {
						var zero T
						return zero, streamFailure(prompt.ID(), ErrorReader, "search selection", ErrReader)
					}
					if err := state.query.applyKey(event); err != nil {
						var zero T
						return zero, streamFailure(prompt.ID(), ErrorReader, "search selection", err)
					}
					state.filter()
				}
			case KeyBackspace, KeyDelete, KeyLeft, KeyRight, KeyWordLeft, KeyWordRight:
				if details.searchPolicy != (SearchPolicy{}) {
					_ = state.query.applyKey(event)
					state.filter()
				}
			case KeyTab:
				if navigation != nil {
					submit = true
				} else {
					state.move(1)
				}
			case KeyShiftTab:
				state.move(-1)
			case KeyNewline:
				// Newlines have no selection meaning unless rebound by the caller.
			case KeyIgnored:
				// An old chord for a rebound meaning is intentionally inert.
			}
		default:
			var zero T
			return zero, streamFailure(prompt.ID(), ErrorReader, "read selection event", ErrReader)
		}
		if submit {
			input, ok := state.submission()
			if ok {
				value, submitErr := prompt.definition.parse(input)
				if submitErr == nil {
					value, submitErr = applyPipeline(ctx, prompt.definition, value, execution.Dependencies, false)
				} else {
					submitErr = validationFailure(prompt.ID(), submitErr, SecretNone)
				}
				if submitErr == nil {
					navigation.captureSelection(state.replay())
					return value, nil
				}
				if !errors.Is(submitErr, ErrValidationExhausted) {
					var zero T
					return zero, submitErr
				}
				attempts++
				state.message = validationMessage(submitErr)
				if !prompt.definition.retry.Unlimited && attempts >= prompt.definition.retry.MaxAttempts {
					var zero T
					return zero, submitErr
				}
			}
		}
		if err := writeSelection(execution, prompt.definition, state); err != nil {
			var zero T
			return zero, err
		}
	}
}

func newSelectionState(details selectionDetails, width, height int) selectionState {
	state := selectionState{
		details: details, selected: make(map[string]bool), width: width, height: height,
		query: lineEditor{maxBytes: max(1, details.searchPolicy.MaxQueryRunes*4)},
	}
	for _, id := range details.initialIDs {
		if id != "" {
			state.selected[id] = true
		}
	}
	state.filter()
	if !details.multiple && len(details.initialIDs) > 0 {
		for position, index := range state.visible {
			if details.options[index].id == details.initialIDs[0] {
				state.focus = position
				break
			}
		}
	}
	state.ensureEnabled(1)
	return state
}

func (state *selectionState) filter() {
	query := normalizeSearchText(state.query.text())
	tokens := strings.Fields(query)
	type match struct{ index, rank int }
	matches := make([]match, 0, len(state.details.options))
	for index, option := range state.details.options {
		rank, ok := selectionRank(option, query, tokens)
		if ok {
			matches = append(matches, match{index: index, rank: rank})
		}
	}
	sort.SliceStable(matches, func(left, right int) bool { return matches[left].rank < matches[right].rank })
	limit := len(matches)
	if policy := state.details.searchPolicy; policy != (SearchPolicy{}) && limit > policy.MaxResults {
		limit = policy.MaxResults
	}
	state.visible = make([]int, limit)
	for index := range limit {
		state.visible[index] = matches[index].index
	}
	state.focus = 0
	state.ensureEnabled(1)
}

func (state *selectionState) replay() selectionReplay {
	replay := selectionReplay{query: state.query.text()}
	for _, option := range state.details.options {
		if state.selected[option.id] {
			replay.selected = append(replay.selected, option.id)
		}
	}
	if state.focus >= 0 && state.focus < len(state.visible) {
		replay.focusID = state.details.options[state.visible[state.focus]].id
	}

	return replay
}

func (state *selectionState) applyReplay(replay selectionReplay) {
	state.selected = make(map[string]bool, len(replay.selected))
	for _, identity := range replay.selected {
		for _, option := range state.details.options {
			if option.id == identity && !option.disabled {
				state.selected[identity] = true
				break
			}
		}
	}
	state.query = lineEditor{maxBytes: max(1, state.details.searchPolicy.MaxQueryRunes*4)}
	_ = state.query.insert(replay.query, false)
	state.filter()
	for position, index := range state.visible {
		if state.details.options[index].id == replay.focusID {
			state.focus = position
			break
		}
	}
	state.ensureEnabled(1)
}

func selectionRank(option selectionOption, query string, tokens []string) (int, bool) {
	label := normalizeSearchText(option.label)
	searchable := strings.TrimSpace(label + " " + normalizeSearchText(option.description))
	if query == "" {
		return 4, true
	}
	if label == query {
		return 0, true
	}
	if strings.HasPrefix(label, query) {
		return 1, true
	}
	candidates := strings.Fields(searchable)
	if tokensMatch(tokens, candidates, strings.HasPrefix) {
		return 2, true
	}
	if tokensMatch(tokens, candidates, strings.Contains) {
		return 3, true
	}
	return 0, false
}

func (state *selectionState) ensureEnabled(direction int) {
	if len(state.visible) == 0 {
		return
	}
	for range len(state.visible) {
		if !state.details.options[state.visible[state.focus]].disabled {
			return
		}
		state.focus = (state.focus + direction + len(state.visible)) % len(state.visible)
	}
}

func (state *selectionState) move(distance int) {
	if len(state.visible) == 0 {
		return
	}
	direction := 1
	if distance < 0 {
		direction = -1
	}
	for range max(1, abs(distance)) {
		state.focus = (state.focus + direction + len(state.visible)) % len(state.visible)
		state.ensureEnabled(direction)
	}
}

func (state *selectionState) focusFirst() {
	state.focus = 0
	state.ensureEnabled(1)
}

func (state *selectionState) focusLast() {
	if len(state.visible) == 0 {
		return
	}
	state.focus = len(state.visible) - 1
	state.ensureEnabled(-1)
}

func (state *selectionState) toggle() {
	if len(state.visible) == 0 {
		return
	}
	option := state.details.options[state.visible[state.focus]]
	if option.disabled {
		return
	}
	if state.selected[option.id] {
		delete(state.selected, option.id)
		state.message = ""
		return
	}
	if len(state.selected) >= state.details.maximum {
		state.message = "Maximum selections reached"
		return
	}
	state.selected[option.id] = true
	state.message = ""
}

func (state *selectionState) submission() (string, bool) {
	if !state.details.multiple {
		if len(state.visible) == 0 || state.details.options[state.visible[state.focus]].disabled {
			state.message = "No selectable options"
			return "", false
		}
		return state.details.options[state.visible[state.focus]].id, true
	}
	identities := make([]string, 0, len(state.selected))
	for _, option := range state.details.options {
		if state.selected[option.id] {
			identities = append(identities, option.id)
		}
	}
	return strings.Join(identities, ","), true
}

func (state *selectionState) pageSize() int {
	reserved := 1
	if state.details.searchPolicy != (SearchPolicy{}) {
		reserved++
	}
	reserved += state.metadata
	if state.message != "" {
		reserved++
	}
	return max(1, state.height-reserved)
}

func writeSelection[T any](execution Execution, definition definition[T], state selectionState) error {
	renderer := execution.Renderer
	if renderer == nil {
		renderer = PlainRenderer{Theme: execution.Theme}
		if execution.Capabilities.Color != ColorNone {
			renderer = ANSIRenderer{Theme: execution.Theme}
		}
	}
	lines := []SemanticLine{Line(Text(RoleLabel, presentationLabel(definition)))}
	lines = append(lines, presentationMetadata(definition)...)
	if state.details.searchPolicy != (SearchPolicy{}) {
		lines = append(lines, Line(Text(RoleHint, "search: "+state.query.text())))
	}
	pageSize := state.pageSize()
	start := 0
	if state.focus >= pageSize {
		start = (state.focus / pageSize) * pageSize
	}
	end := min(len(state.visible), start+pageSize)
	for position := start; position < end; position++ {
		option := state.details.options[state.visible[position]]
		segments := make([]Segment, 0, 2)
		if position == state.focus && !option.disabled {
			segments = append(segments, Text(RoleFocus, ""))
		}
		role := RoleValue
		if option.disabled {
			role = RoleDisabled
		} else if state.selected[option.id] {
			role = RoleSelected
		}
		label := option.label
		if option.group != "" {
			label = "[" + option.group + "] " + label
		}
		segments = append(segments, Text(role, label))
		if option.description != "" {
			segments = append(segments, Text(RoleValue, " - "), Text(RoleHint, option.description))
		}
		lines = append(lines, Line(segments...))
	}
	if state.message != "" {
		lines = append(lines, Line(Text(RoleError, state.message)))
	}
	output, err := renderer.Render(NewFrame(lines...), RenderOptions{
		Width: state.width, Color: execution.Capabilities.Color,
		ASCIIOnly:  !execution.Capabilities.Unicode,
		Hyperlinks: execution.Capabilities.Hyperlinks,
	})
	if err != nil {
		return streamFailure(definition.id, ErrorRenderer, "render selection", err)
	}
	if _, err = io.WriteString(execution.Output, output); err != nil {
		return streamFailure(definition.id, ErrorWriter, "write selection", err)
	}
	return nil
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
