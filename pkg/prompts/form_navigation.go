package prompts

import (
	"context"
	"errors"
)

var errFormBack = errors.New("navigate to previous form field")

type formNavigationContextKey struct{}

type formReplayKind uint8

const (
	formReplayText formReplayKind = iota
	formReplayBytes
	formReplaySelection
)

type selectionReplay struct {
	selected []string
	focusID  string
	query    string
}

type formReplay struct {
	kind      formReplayKind
	text      string
	bytes     *SecretBytes
	selection selectionReplay
}

func (replay *formReplay) destroy() {
	if replay == nil {
		return
	}
	replay.bytes.Destroy()
	replay.bytes = nil
	replay.text = ""
	replay.selection = selectionReplay{}
}

type formInteraction struct {
	initial  *formReplay
	captured *formReplay
}

func formInteractionFrom(ctx context.Context) *formInteraction {
	interaction, _ := ctx.Value(formNavigationContextKey{}).(*formInteraction)

	return interaction
}

func (interaction *formInteraction) captureText(value string) {
	if interaction == nil {
		return
	}
	interaction.replace(&formReplay{kind: formReplayText, text: value})
}

func (interaction *formInteraction) captureBytes(value []byte) {
	if interaction == nil {
		return
	}
	interaction.replace(&formReplay{kind: formReplayBytes, bytes: NewSecretBytes(value)})
}

func (interaction *formInteraction) captureSelection(value selectionReplay) {
	if interaction == nil {
		return
	}
	value.selected = append([]string(nil), value.selected...)
	interaction.replace(&formReplay{kind: formReplaySelection, selection: value})
}

func (interaction *formInteraction) replace(replay *formReplay) {
	interaction.captured = replay
}
