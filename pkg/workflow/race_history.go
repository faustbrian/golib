package workflow

import (
	"sort"
	"time"
)

// RaceProgress is one immutable durably selected race winner.
type RaceProgress struct {
	stepName       string
	winnerStepName string
	decidedAt      time.Time
}

// StepName returns the stable race control-step name.
func (progress RaceProgress) StepName() string { return progress.stepName }

// WinnerStepName returns the persisted winning branch name.
func (progress RaceProgress) WinnerStepName() string { return progress.winnerStepName }

// DecidedAt returns when the winner decision became durable history.
func (progress RaceProgress) DecidedAt() time.Time { return progress.decidedAt }

func validRaceEventFields(spec HistoryEventSpec) bool {
	return stableName.MatchString(spec.StepName) && stableName.MatchString(string(spec.Data)) &&
		spec.Definition == (DefinitionReference{}) && spec.SuccessorID == "" && spec.Attempt == 0 &&
		spec.IdempotencyKey == "" && spec.DueAt.IsZero() && spec.Code == "" && !spec.Retryable
}

func (instance *Instance) applyRace(registry *Registry, event HistoryEvent) error {
	definition, _ := registry.Resolve(instance.definition.Name(), instance.definition.Version())
	step, ok := definitionStep(definition, event.stepName, StepRace)
	winner := string(event.data)
	_, winnerObserved := instance.signals[winner]
	if !ok || instance.status != StatusRunning || !winnerObserved ||
		!containsBranch(step.Branches, winner) {
		return ErrInvalidTransition
	}
	if _, exists := instance.races[event.stepName]; exists {
		return ErrInvalidTransition
	}
	instance.races[event.stepName] = RaceProgress{
		stepName: event.stepName, winnerStepName: winner, decidedAt: event.occurredAt,
	}
	return nil
}

func containsBranch(branches []string, candidate string) bool {
	for _, branch := range branches {
		if branch == candidate {
			return true
		}
	}
	return false
}

func sortedRaceProgress(progress map[string]RaceProgress) []RaceProgress {
	if len(progress) == 0 {
		return nil
	}
	names := make([]string, 0, len(progress))
	for name := range progress {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]RaceProgress, 0, len(names))
	for _, name := range names {
		result = append(result, progress[name])
	}
	return result
}

type raceProgressSnapshot struct {
	StepName       string
	WinnerStepName string
	DecidedAt      time.Time
}

func raceProgressSnapshots(progress map[string]RaceProgress) []raceProgressSnapshot {
	races := sortedRaceProgress(progress)
	if races == nil {
		return nil
	}
	result := make([]raceProgressSnapshot, 0, len(races))
	for _, race := range races {
		result = append(result, raceProgressSnapshot{
			StepName: race.stepName, WinnerStepName: race.winnerStepName, DecidedAt: race.decidedAt,
		})
	}
	return result
}
