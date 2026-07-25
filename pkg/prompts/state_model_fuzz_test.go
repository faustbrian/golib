package prompts

import (
	"strings"
	"testing"
)

func FuzzLineEditorMatchesReferenceModel(fuzz *testing.F) {
	fuzz.Add([]byte{0, 1, 2, 6, 4, 7, 3, 10, 11})
	fuzz.Add([]byte{3, 3, 8, 5, 9, 2})
	fuzz.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 256 {
			t.Skip()
		}
		editor := lineEditor{maxBytes: 64}
		cells := []string{}
		cursor, byteCount := 0, 0
		insert := func(value string) {
			wantError := byteCount+len(value) > editor.maxBytes
			err := editor.insert(value, false)
			if wantError {
				if err == nil {
					t.Fatalf("insert %q exceeded the model byte bound", value)
				}
				return
			}
			if err != nil {
				t.Fatalf("insert %q error = %v", value, err)
			}
			cells = append(cells, "")
			copy(cells[cursor+1:], cells[cursor:])
			cells[cursor] = value
			cursor++
			byteCount += len(value)
		}
		for index, value := range raw {
			switch value % 12 {
			case 0:
				insert(string(rune('a' + value%26)))
			case 1:
				insert(" ")
			case 2:
				insert("e\u0301")
			case 3:
				insert("👩‍💻")
			case 4:
				if err := editor.applyKey(KeyEvent(KeyBackspace)); err != nil {
					t.Fatal(err)
				}
				if cursor > 0 {
					byteCount -= len(cells[cursor-1])
					cells = append(cells[:cursor-1], cells[cursor:]...)
					cursor--
				}
			case 5:
				if err := editor.applyKey(KeyEvent(KeyDelete)); err != nil {
					t.Fatal(err)
				}
				if cursor < len(cells) {
					byteCount -= len(cells[cursor])
					cells = append(cells[:cursor], cells[cursor+1:]...)
				}
			case 6:
				_ = editor.applyKey(KeyEvent(KeyLeft))
				if cursor > 0 {
					cursor--
				}
			case 7:
				_ = editor.applyKey(KeyEvent(KeyRight))
				if cursor < len(cells) {
					cursor++
				}
			case 8:
				_ = editor.applyKey(KeyEvent(KeyHome))
				cursor = 0
			case 9:
				_ = editor.applyKey(KeyEvent(KeyEnd))
				cursor = len(cells)
			case 10:
				_ = editor.applyKey(KeyEvent(KeyWordLeft))
				for cursor > 0 && strings.TrimSpace(cells[cursor-1]) == "" {
					cursor--
				}
				for cursor > 0 && strings.TrimSpace(cells[cursor-1]) != "" {
					cursor--
				}
			case 11:
				_ = editor.applyKey(KeyEvent(KeyWordRight))
				for cursor < len(cells) && strings.TrimSpace(cells[cursor]) != "" {
					cursor++
				}
				for cursor < len(cells) && strings.TrimSpace(cells[cursor]) == "" {
					cursor++
				}
			}
			if editor.cursor != cursor || editor.text() != strings.Join(cells, "") {
				t.Fatalf("operation %d editor = %q at %d; model = %q at %d",
					index, editor.text(), editor.cursor, strings.Join(cells, ""), cursor)
			}
		}
	})
}

func FuzzSelectionFilterAndStateMatchesReferenceModel(fuzz *testing.F) {
	fuzz.Add([]byte{0, 1, 2, 6, 4, 8, 3})
	fuzz.Add([]byte{7, 5, 1, 9, 2})
	labels := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"}
	options := make([]selectionOption, len(labels))
	for index, label := range labels {
		options[index] = selectionOption{id: label, label: label, disabled: index == 2}
	}
	details := selectionDetails{
		options: options, initialIDs: []string{"alpha"}, multiple: true,
		minimum: 0, maximum: len(options),
		searchPolicy: SearchPolicy{MaxOptions: len(options), MaxResults: len(options), MaxQueryRunes: 16},
	}
	fuzz.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 256 {
			t.Skip()
		}
		state := newSelectionState(details, 40, 5)
		visible := []int{0, 1, 2, 3, 4, 5}
		focus, height := 0, 5
		selected := map[string]bool{"alpha": true}
		ensureEnabled := func(direction int) {
			if len(visible) == 0 {
				return
			}
			for range visible {
				if !options[visible[focus]].disabled {
					return
				}
				focus = (focus + direction + len(visible)) % len(visible)
			}
		}
		move := func(distance int) {
			if len(visible) == 0 {
				return
			}
			direction := 1
			if distance < 0 {
				direction = -1
			}
			for range max(1, abs(distance)) {
				focus = (focus + direction + len(visible)) % len(visible)
				ensureEnabled(direction)
			}
		}
		setQuery := func(query string) {
			state.query = lineEditor{maxBytes: 64}
			if err := state.query.insert(query, false); err != nil {
				t.Fatal(err)
			}
			state.filter()
			focus = 0
			visible = visible[:0]
			for index, label := range labels {
				if query == "" || query == label {
					visible = append(visible, index)
				}
			}
			ensureEnabled(1)
		}
		for index, value := range raw {
			switch value % 10 {
			case 0:
				state.move(1)
				move(1)
			case 1:
				state.move(-1)
				move(-1)
			case 2:
				state.move(state.pageSize())
				move(max(1, height-2))
			case 3:
				state.move(-state.pageSize())
				move(-max(1, height-2))
			case 4:
				state.focusFirst()
				focus = 0
				ensureEnabled(1)
			case 5:
				state.focusLast()
				focus = len(visible) - 1
				ensureEnabled(-1)
			case 6:
				state.toggle()
				option := options[visible[focus]]
				if !option.disabled {
					selected[option.id] = !selected[option.id]
					if !selected[option.id] {
						delete(selected, option.id)
					}
				}
			case 7:
				setQuery("")
			case 8:
				setQuery(labels[int(value)%len(labels)])
			case 9:
				height = int(value % 8)
				state.height = height
			}
			assertSelectionModel(t, index, state, options, visible, focus, selected)
		}
	})
}

func assertSelectionModel(
	t *testing.T,
	operation int,
	state selectionState,
	options []selectionOption,
	visible []int,
	focus int,
	selected map[string]bool,
) {
	t.Helper()
	if len(state.visible) != len(visible) || state.focus != focus || len(state.selected) != len(selected) {
		t.Fatalf("operation %d state = %#v; model visible=%v focus=%d selected=%v",
			operation, state, visible, focus, selected)
	}
	for index := range visible {
		if state.visible[index] != visible[index] {
			t.Fatalf("operation %d visible = %v; model = %v", operation, state.visible, visible)
		}
	}
	for _, option := range options {
		if state.selected[option.id] != selected[option.id] {
			t.Fatalf("operation %d selection %q = %t; model = %t",
				operation, option.id, state.selected[option.id], selected[option.id])
		}
	}
}
