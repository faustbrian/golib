package mocks

//go:generate mockgen -package=mocks -destination=mock_worker.go github.com/faustbrian/golib/pkg/queue/core Worker
//go:generate mockgen -package=mocks -destination=mock_queued_message.go github.com/faustbrian/golib/pkg/queue/core QueuedMessage
//go:generate mockgen -package=mocks -destination=mock_task_message.go github.com/faustbrian/golib/pkg/queue/core TaskMessage
