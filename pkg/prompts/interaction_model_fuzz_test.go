package prompts_test

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func FuzzInteractiveSelectionPaginationMatchesModel(fuzz *testing.F) {
	fuzz.Add([]byte{0, 0, 2, 7, 5})
	fuzz.Add([]byte{4, 1, 3, 6, 2})
	options := make([]prompts.Option[int], 0, 8)
	for index := range 8 {
		option, err := prompts.NewOption(prompts.OptionConfig[int]{
			ID: fmt.Sprintf("option-%d", index), Label: fmt.Sprintf("Option %d", index), Value: index,
		})
		if err != nil {
			panic(err)
		}
		options = append(options, option)
	}
	prompt, err := prompts.NewSelect(prompts.SelectConfig[int]{
		ID: "option", Label: "Option", Options: options, InitialID: "option-0",
	})
	if err != nil {
		panic(err)
	}

	fuzz.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 256 {
			t.Skip()
		}
		focus, height := 0, 4
		events := make([]prompts.InputEvent, 0, len(raw)+1)
		move := func(distance int) {
			direction := 1
			if distance < 0 {
				direction = -1
			}
			for range max(1, absolute(distance)) {
				focus = (focus + direction + len(options)) % len(options)
			}
		}
		for _, value := range raw {
			switch value % 7 {
			case 0:
				events = append(events, prompts.KeyEvent(prompts.KeyDown))
				move(1)
			case 1:
				events = append(events, prompts.KeyEvent(prompts.KeyUp))
				move(-1)
			case 2:
				events = append(events, prompts.KeyEvent(prompts.KeyPageDown))
				move(max(1, height-1))
			case 3:
				events = append(events, prompts.KeyEvent(prompts.KeyPageUp))
				move(-max(1, height-1))
			case 4:
				events = append(events, prompts.KeyEvent(prompts.KeyHome))
				focus = 0
			case 5:
				events = append(events, prompts.KeyEvent(prompts.KeyEnd))
				focus = len(options) - 1
			case 6:
				height = int(value % 7)
				events = append(events, prompts.ResizeEvent(20+int(value%40), height))
			}
		}
		events = append(events, prompts.KeyEvent(prompts.KeyEnter))
		terminal := prompts.NewVirtualTerminal(40, 4)
		if err := terminal.Push(events...); err != nil {
			t.Fatalf("Push() error = %v", err)
		}
		value, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
		if err != nil || value != focus {
			t.Fatalf("Run() = %d, %v; model focus = %d", value, err, focus)
		}
		if !terminal.Released() {
			t.Fatal("terminal was not released")
		}
	})
}

func FuzzFormNavigationAndVisibilityMatchesModel(fuzz *testing.F) {
	fuzz.Add([]byte("Ada"), []byte("retained"), true)
	fuzz.Add([]byte{0, 0xff}, []byte("hidden"), false)
	fuzz.Fuzz(func(t *testing.T, rawName, rawDetails []byte, revisit bool) {
		if len(rawName) > 128 || len(rawDetails) > 128 {
			t.Skip()
		}
		name := hex.EncodeToString(rawName)
		details := hex.EncodeToString(rawDetails)
		namePrompt, err := prompts.NewText(prompts.TextConfig{ID: "name", Label: "Name"})
		if err != nil {
			t.Fatal(err)
		}
		advanced, err := prompts.NewConfirm(prompts.ConfirmConfig{ID: "advanced", Label: "Advanced"})
		if err != nil {
			t.Fatal(err)
		}
		detailsPrompt, err := prompts.NewText(prompts.TextConfig{ID: "details", Label: "Details"})
		if err != nil {
			t.Fatal(err)
		}
		form, err := prompts.NewForm(prompts.FormConfig{
			ID: "setup",
			Fields: []prompts.FormField{
				prompts.AsField(namePrompt), prompts.AsField(advanced),
				prompts.When(prompts.AsField(detailsPrompt), func(result prompts.FormResult) bool {
					value, ok := prompts.FormValue[bool](result, "advanced")
					return ok && value
				}),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		events := []prompts.InputEvent{
			prompts.PasteEvent(name), prompts.KeyEvent(prompts.KeyTab),
		}
		wantAdvanced := revisit
		if revisit {
			events = append(events,
				prompts.PasteEvent("yes"), prompts.KeyEvent(prompts.KeyEnter),
				prompts.PasteEvent(details), prompts.KeyEvent(prompts.KeyShiftTab),
				prompts.KeyEvent(prompts.KeyHome), prompts.KeyEvent(prompts.KeyDelete),
				prompts.KeyEvent(prompts.KeyDelete), prompts.KeyEvent(prompts.KeyDelete),
				prompts.PasteEvent("no"), prompts.KeyEvent(prompts.KeyEnter),
			)
			wantAdvanced = false
		} else {
			events = append(events, prompts.PasteEvent("no"), prompts.KeyEvent(prompts.KeyEnter))
		}
		terminal := prompts.NewVirtualTerminal(40, 8)
		if err := terminal.Push(events...); err != nil {
			t.Fatalf("Push() error = %v", err)
		}
		terminal.CloseInput()
		result, err := prompts.RunForm(context.Background(), form, interactiveExecution(terminal))
		if err != nil {
			t.Fatalf("RunForm() error = %v", err)
		}
		gotName, nameOK := prompts.FormValue[string](result, "name")
		gotAdvanced, advancedOK := prompts.FormValue[bool](result, "advanced")
		if !nameOK || gotName != name || !advancedOK || gotAdvanced != wantAdvanced {
			t.Fatalf("result = %v, name %q, advanced %t", result.IDs(), gotName, gotAdvanced)
		}
		if result.Has("details") {
			t.Fatalf("hidden field survived navigation: %v", result.IDs())
		}
	})
}

func FuzzDynamicOptionGenerationsMatchModel(fuzz *testing.F) {
	fuzz.Add([]byte{0, 1, 2, 3, 4, 5})
	fuzz.Add([]byte{9, 9, 1, 7, 2})
	fuzz.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 256 {
			t.Skip()
		}
		clock := prompts.NewVirtualClock(time.Unix(100, 0))
		dynamic, err := prompts.NewDynamicOptions(prompts.DynamicOptionsConfig[string]{
			Clock: clock, Debounce: 2 * time.Millisecond, MaxOptions: 1, MaxQueryRunes: 8,
			Provider: func(_ context.Context, query string) ([]prompts.Option[string], error) {
				option, optionErr := prompts.NewOption(prompts.OptionConfig[string]{
					ID: query, Label: query, Value: query,
				})
				return []prompts.Option[string]{option}, optionErr
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		var generations []prompts.QueryGeneration
		var current prompts.QueryGeneration
		var due, now int64
		appliedQuery := ""
		queries := make(map[prompts.QueryGeneration]string)
		for _, value := range raw {
			switch value % 3 {
			case 0:
				query := fmt.Sprintf("q%02x", value)
				generation, scheduleErr := dynamic.Schedule(query)
				if scheduleErr != nil {
					t.Fatalf("Schedule() error = %v", scheduleErr)
				}
				current, due = generation, now+2
				queries[generation] = query
				generations = append(generations, generation)
			case 1:
				delta := int64(value%4) + 1
				now += delta
				if err := clock.Advance(time.Duration(delta) * time.Millisecond); err != nil {
					t.Fatalf("Advance() error = %v", err)
				}
			case 2:
				generation := current
				if len(generations) > 0 {
					generation = generations[int(value)%len(generations)]
				}
				options, applied, resolveErr := dynamic.Resolve(context.Background(), generation)
				if resolveErr != nil {
					t.Fatalf("Resolve() error = %v", resolveErr)
				}
				wantApplied := generation != 0 && generation == current && now >= due
				if applied != wantApplied {
					t.Fatalf("Resolve(%d) applied = %t; want %t", generation, applied, wantApplied)
				}
				if wantApplied {
					appliedQuery = queries[generation]
					if len(options) != 1 || options[0].ID() != appliedQuery {
						t.Fatalf("Resolve() options = %#v; want %q", options, appliedQuery)
					}
				}
			}
			snapshot, generation := dynamic.Snapshot()
			if generation != current || (appliedQuery == "" && len(snapshot) != 0) ||
				(appliedQuery != "" && (len(snapshot) != 1 || snapshot[0].ID() != appliedQuery)) {
				t.Fatalf("Snapshot() = %#v, %d; model = %q, %d", snapshot, generation, appliedQuery, current)
			}
		}
	})
}

func FuzzProgressLifecycleMatchesModel(fuzz *testing.F) {
	fuzz.Add([]byte{0, 4, 1, 3, 2, 7, 4})
	fuzz.Add([]byte{1, 31, 0, 32, 3})
	fuzz.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 256 {
			t.Skip()
		}
		progress, err := prompts.NewProgress(prompts.ProgressConfig{ID: "work", Label: "Work", Total: 32})
		if err != nil {
			t.Fatal(err)
		}
		current := int64(0)
		state := prompts.ProgressPending
		terminal := false
		for index, value := range raw {
			candidate := int64(value % 40)
			switch value % 5 {
			case 0:
				updateErr := progress.Update(candidate, "update")
				accepted := !terminal && candidate >= current && candidate <= 32
				if accepted {
					current, state = candidate, prompts.ProgressRunning
				}
				assertMutationResult(t, updateErr, accepted, index)
			case 1:
				delta := candidate % 9
				updateErr := progress.Increment(delta, "increment")
				accepted := !terminal && current+delta <= 32
				if accepted {
					current, state = current+delta, prompts.ProgressRunning
				}
				assertMutationResult(t, updateErr, accepted, index)
			case 2:
				progress.Complete("complete")
				if !terminal {
					state, terminal = prompts.ProgressSucceeded, true
				}
			case 3:
				progress.Fail("failed")
				if !terminal {
					state, terminal = prompts.ProgressFailed, true
				}
			case 4:
				progress.Cancel("canceled")
				if !terminal {
					state, terminal = prompts.ProgressCanceled, true
				}
			}
			snapshot := progress.Snapshot()
			if snapshot.Current != current || snapshot.State != state || snapshot.Total != 32 {
				t.Fatalf("operation %d snapshot = %#v; model current=%d state=%d", index, snapshot, current, state)
			}
		}
	})
}

func assertMutationResult(t *testing.T, err error, accepted bool, operation int) {
	t.Helper()
	if accepted && err != nil {
		t.Fatalf("operation %d unexpectedly failed: %v", operation, err)
	}
	if !accepted && !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("operation %d error = %v; want invalid definition", operation, err)
	}
}

func absolute(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
