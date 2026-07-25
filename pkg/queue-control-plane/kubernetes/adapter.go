// Package kubernetes exposes the deliberately narrow Kubernetes integration.
package kubernetes

import (
	"context"
	"errors"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

var (
	// ErrInvalidConfiguration reports an unscoped Kubernetes adapter.
	ErrInvalidConfiguration = errors.New("kubernetes: invalid configuration")
	// ErrInvalidWorkload reports a malformed Kubernetes Deployment name.
	ErrInvalidWorkload = errors.New("kubernetes: invalid workload")
	// ErrInvalidPage reports an unbounded Deployment list request.
	ErrInvalidPage = errors.New("kubernetes: invalid page")
	// ErrInvalidResponse reports malformed or cross-namespace Kubernetes state.
	ErrInvalidResponse = errors.New("kubernetes: invalid response")
)

const (
	// MaxPageSize bounds one Kubernetes Deployment request.
	MaxPageSize int64 = 500
	// MaxContinueTokenBytes bounds the opaque Kubernetes pagination token.
	MaxContinueTokenBytes = 4_096
)

// DeploymentClient is the deliberately restricted part of a namespace-scoped
// Kubernetes Deployment client used by the control plane.
type DeploymentClient interface {
	Get(context.Context, string, metav1.GetOptions) (*appsv1.Deployment, error)
	List(context.Context, metav1.ListOptions) (*appsv1.DeploymentList, error)
	GetScale(context.Context, string, metav1.GetOptions) (*autoscalingv1.Scale, error)
	UpdateScale(context.Context, string, *autoscalingv1.Scale, metav1.UpdateOptions) (*autoscalingv1.Scale, error)
}

// Adapter reports Deployment status from one configured namespace.
type Adapter struct {
	namespace string
	client    DeploymentClient
}

// Status is the bounded Deployment state presented by the control plane.
type Status struct {
	Namespace           string `json:"namespace"`
	Name                string `json:"name"`
	Generation          int64  `json:"generation"`
	ObservedGeneration  int64  `json:"observed_generation"`
	DesiredReplicas     int32  `json:"desired_replicas"`
	UpdatedReplicas     int32  `json:"updated_replicas"`
	ReadyReplicas       int32  `json:"ready_replicas"`
	AvailableReplicas   int32  `json:"available_replicas"`
	UnavailableReplicas int32  `json:"unavailable_replicas"`
}

// Page is one bounded page of Deployment status.
type Page struct {
	Items     []Status `json:"items"`
	Continue  string   `json:"continue,omitempty"`
	Remaining int64    `json:"remaining"`
}

// ScaleResult is the Kubernetes acknowledgement of an authorized replica
// update.
type ScaleResult struct {
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	DesiredReplicas int32  `json:"desired_replicas"`
	CurrentReplicas int32  `json:"current_replicas"`
	ResourceVersion string `json:"resource_version"`
}

// New creates an adapter scoped to namespace.
func New(namespace string, client DeploymentClient) (*Adapter, error) {
	if len(validation.IsDNS1123Label(namespace)) != 0 || nilInterface(client) {
		return nil, ErrInvalidConfiguration
	}

	return &Adapter{namespace: namespace, client: client}, nil
}

// Get returns the current status of one Deployment.
func (adapter *Adapter) Get(ctx context.Context, name string) (Status, error) {
	if len(validation.IsDNS1123Subdomain(name)) != 0 {
		return Status{}, ErrInvalidWorkload
	}

	deployment, err := adapter.client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Status{}, err
	}
	if deployment == nil || deployment.Name != name || !adapter.validDeployment(deployment) {
		return Status{}, ErrInvalidResponse
	}

	return statusOf(deployment), nil
}

// List returns one server-paginated page of Deployment status.
func (adapter *Adapter) List(ctx context.Context, limit int64, continueToken string) (Page, error) {
	if limit < 1 || limit > MaxPageSize || len(continueToken) > MaxContinueTokenBytes {
		return Page{}, ErrInvalidPage
	}

	deployments, err := adapter.client.List(ctx, metav1.ListOptions{
		Limit:    limit,
		Continue: continueToken,
	})
	if err != nil {
		return Page{}, err
	}
	if deployments == nil {
		return Page{}, ErrInvalidResponse
	}

	page := Page{
		Items:    make([]Status, 0, len(deployments.Items)),
		Continue: deployments.Continue,
	}
	if deployments.RemainingItemCount != nil {
		page.Remaining = *deployments.RemainingItemCount
	}
	for index := range deployments.Items {
		if !adapter.validDeployment(&deployments.Items[index]) {
			return Page{}, ErrInvalidResponse
		}
		page.Items = append(page.Items, statusOf(&deployments.Items[index]))
	}

	return page, nil
}

// Scale updates only the Deployment scale subresource. Authorization and
// scale-to-zero confirmation are enforced by the control service before this
// adapter is called.
func (adapter *Adapter) Scale(ctx context.Context, name string, replicas uint32) (ScaleResult, error) {
	if len(validation.IsDNS1123Subdomain(name)) != 0 || replicas > controlplane.MaxScaleReplicas {
		return ScaleResult{}, ErrInvalidWorkload
	}

	scale, err := adapter.client.GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ScaleResult{}, err
	}
	if scale == nil {
		return ScaleResult{}, ErrInvalidResponse
	}

	update := scale.DeepCopy()
	update.Spec.Replicas = int32(replicas)
	updated, err := adapter.client.UpdateScale(ctx, name, update, metav1.UpdateOptions{})
	if err != nil {
		return ScaleResult{}, err
	}
	if updated == nil {
		return ScaleResult{}, ErrInvalidResponse
	}

	return ScaleResult{
		Namespace:       adapter.namespace,
		Name:            name,
		DesiredReplicas: updated.Spec.Replicas,
		CurrentReplicas: updated.Status.Replicas,
		ResourceVersion: updated.ResourceVersion,
	}, nil
}

func (adapter *Adapter) validDeployment(deployment *appsv1.Deployment) bool {
	return deployment.Namespace == adapter.namespace &&
		len(validation.IsDNS1123Subdomain(deployment.Name)) == 0
}

func statusOf(deployment *appsv1.Deployment) Status {
	desired := int32(0)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}

	return Status{
		Namespace:           deployment.Namespace,
		Name:                deployment.Name,
		Generation:          deployment.Generation,
		ObservedGeneration:  deployment.Status.ObservedGeneration,
		DesiredReplicas:     desired,
		UpdatedReplicas:     deployment.Status.UpdatedReplicas,
		ReadyReplicas:       deployment.Status.ReadyReplicas,
		AvailableReplicas:   deployment.Status.AvailableReplicas,
		UnavailableReplicas: deployment.Status.UnavailableReplicas,
	}
}
