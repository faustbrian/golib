package contextstore

type auditValue = int
type auditBase struct{}
type auditEmbedded struct{ auditBase }
type auditPort interface{ Audit() }
type auditBox[T any] struct{ Value T }

func auditCallback[T any](value T) T {
	callback := func(candidate T) T { return candidate }
	return callback(value)
}
