package eventqueue

import (
	"fmt"
	"strconv"
)

func formatRedactedError(state fmt.State, verb rune, message string) {
	if verb == 'q' {
		message = strconv.Quote(message)
	}
	_, _ = state.Write([]byte(message))
}
