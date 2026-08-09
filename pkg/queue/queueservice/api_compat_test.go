package queueservice_test

import (
	"context"

	"github.com/faustbrian/golib/pkg/correlation"
	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/queueservice"
	"github.com/faustbrian/golib/pkg/service"
	"go.opentelemetry.io/otel/propagation"
)

type apiProducer struct{}

var (
	_ func(queueservice.ProducerOptions[*apiProducer]) (*queueservice.Producer[*apiProducer], error) = queueservice.NewProducer[*apiProducer]
	_ func(*queueservice.Producer[*apiProducer]) *apiProducer                                        = (*queueservice.Producer[*apiProducer]).Resource
	_ func(*queueservice.Producer[*apiProducer]) service.Component                                   = (*queueservice.Producer[*apiProducer]).Component
	_ func(*queueservice.Producer[*apiProducer]) (service.ReadinessCheck, bool)                      = (*queueservice.Producer[*apiProducer]).Readiness
	_ func(
		*queueservice.Producer[*apiProducer],
		context.Context,
		core.QueuedMessage,
		...job.AllowOption,
	) (correlation.Values, error) = (*queueservice.Producer[*apiProducer]).Publish
	_ func(
		*queueservice.Producer[*apiProducer],
		context.Context,
		core.QueuedMessage,
		...job.AllowOption,
	) (correlation.Values, queueservice.PublishAcceptance, error) = (*queueservice.Producer[*apiProducer]).PublishWithAcceptance

	_ func(queueservice.LifecycleWorkerOptions[*apiProducer]) (*queueservice.LifecycleWorker[*apiProducer], error) = queueservice.NewLifecycleWorker[*apiProducer]
	_ func(*queueservice.LifecycleWorker[*apiProducer]) *apiProducer                                               = (*queueservice.LifecycleWorker[*apiProducer]).Resource
	_ func(*queueservice.LifecycleWorker[*apiProducer]) service.Plan                                               = (*queueservice.LifecycleWorker[*apiProducer]).Plan
	_ queueservice.CloseAdmission[*apiProducer]                                                                    = queueservice.LifecycleWorkerOptions[*apiProducer]{}.CloseAdmission

	_ func(queueservice.HandlerOptions) (queueservice.Handler, error) = queueservice.NewHandler
	_ func(queueservice.WorkerOptions) (*queueservice.Worker, error)  = queueservice.NewWorker
	_ func(*queueservice.Worker) *queue.Queue                         = (*queueservice.Worker).Queue
	_ func(*queueservice.Worker) service.Component                    = (*queueservice.Worker).Component

	_ func(*queue.Queue, context.Context) error = (*queue.Queue).ReleaseContext
	_ func(*queue.Queue, context.Context) error = (*queue.Queue).WaitContext
	_ func(*queue.Queue) error                  = (*queue.Queue).CloseAdmission
	_ func(*job.Message) map[string]string      = (*job.Message).CorrelationMetadata
	_ func(*job.Message) map[string]string      = (*job.Message).TraceContextMetadata
	_ propagation.TextMapPropagator             = queueservice.ProducerOptions[*apiProducer]{}.TracePropagator
	_ propagation.TextMapPropagator             = queueservice.HandlerOptions{}.TracePropagator
	_ error                                     = (*queueservice.CallbackError)(nil)
	_ error                                     = queueservice.ErrWorkerExited
)
