package prompts

// keyCount is the number of declared Key values, including KeyRune.
const keyCount = 21

// KeyBinding assigns one physical semantic key to an existing prompt meaning.
// KeyRune is not configurable because text runes carry their own value.
type KeyBinding struct {
	Input   Key
	Meaning Key
}

// KeyMap is an immutable execution-local key translation table.
type KeyMap struct {
	mappings   [keyCount]Key
	bound      [keyCount]bool
	configured bool
}

// NewKeyMap starts with the default bindings and applies each rebind. Rebinding
// a meaning removes its prior keys so the old shortcut does not remain active.
func NewKeyMap(bindings ...KeyBinding) (KeyMap, error) {
	keyMap := defaultKeyMap()
	seen := [keyCount]bool{}
	for _, binding := range bindings {
		if !bindableKey(binding.Input) || !bindableKey(binding.Meaning) {
			return KeyMap{}, &Error{
				Kind: ErrorInvalidDefinition, Operation: "define key map",
				Cause: ErrInvalidDefinition,
			}
		}
		if seen[binding.Input] {
			return KeyMap{}, &Error{
				Kind: ErrorInvalidDefinition, Operation: "define key map",
				Cause: ErrInvalidDefinition,
			}
		}
		seen[binding.Input] = true
		for input := KeyEnter; input < Key(keyCount); input++ {
			if input == KeyIgnored {
				continue
			}
			if keyMap.bound[input] && keyMap.mappings[input] == binding.Meaning {
				keyMap.bound[input] = false
			}
		}
		keyMap.mappings[binding.Input] = binding.Meaning
		keyMap.bound[binding.Input] = true
	}

	return keyMap, nil
}

func defaultKeyMap() KeyMap {
	keyMap := KeyMap{configured: true}
	for key := KeyEnter; key < Key(keyCount); key++ {
		if key == KeyIgnored {
			continue
		}
		keyMap.mappings[key] = key
		keyMap.bound[key] = true
	}
	keyMap.mappings[KeyCtrlC] = KeyEscape

	return keyMap
}

func (keyMap KeyMap) translate(event InputEvent) InputEvent {
	if event.Key == KeyRune {
		return event
	}
	if event.Key == KeyIgnored || event.Key >= Key(keyCount) {
		return event
	}
	if !keyMap.configured {
		keyMap = defaultKeyMap()
	}
	if keyMap.bound[event.Key] {
		event.Key = keyMap.mappings[event.Key]
	} else {
		event.Key = KeyIgnored
	}

	return event
}

func bindableKey(key Key) bool {
	return key > KeyRune && key < Key(keyCount) && key != KeyIgnored
}
