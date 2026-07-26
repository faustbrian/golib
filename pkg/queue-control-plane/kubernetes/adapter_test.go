package kubernetes

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	validClient := &deploymentClient{}
	var typedNil *deploymentClient
	for name, input := range map[string]struct {
		namespace string
		client    DeploymentClient
	}{
		"missing namespace": {client: validClient},
		"invalid namespace": {namespace: "Not_A_Namespace", client: validClient},
		"missing client":    {namespace: "queues"},
		"typed nil client":  {namespace: "queues", client: typedNil},
	} {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			adapter, err := New(input.namespace, input.client)
			if adapter != nil || !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("New() = (%v, %v), want (nil, ErrInvalidConfiguration)", adapter, err)
			}
		})
	}
}

func TestAdapterRejectsInvalidWorkloadName(t *testing.T) {
	t.Parallel()

	client := &deploymentClient{
		get: func(context.Context, string, metav1.GetOptions) (*appsv1.Deployment, error) {
			t.Fatal("invalid workload reached Kubernetes")

			return nil, nil
		},
	}
	adapter, err := New("queues", client)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, name := range []string{"", "Not_A_Deployment"} {
		if _, err := adapter.Get(context.Background(), name); !errors.Is(err, ErrInvalidWorkload) {
			t.Fatalf("Get(%q) error = %v, want ErrInvalidWorkload", name, err)
		}
	}
}

type deploymentClient struct {
	get         func(context.Context, string, metav1.GetOptions) (*appsv1.Deployment, error)
	list        func(context.Context, metav1.ListOptions) (*appsv1.DeploymentList, error)
	getScale    func(context.Context, string, metav1.GetOptions) (*autoscalingv1.Scale, error)
	updateScale func(context.Context, string, *autoscalingv1.Scale, metav1.UpdateOptions) (*autoscalingv1.Scale, error)
}

func (client *deploymentClient) Get(
	ctx context.Context,
	name string,
	options metav1.GetOptions,
) (*appsv1.Deployment, error) {
	return client.get(ctx, name, options)
}

func (client *deploymentClient) List(
	ctx context.Context,
	options metav1.ListOptions,
) (*appsv1.DeploymentList, error) {
	return client.list(ctx, options)
}

func TestAdapterListsBoundedDeploymentStatus(t *testing.T) {
	t.Parallel()

	one := int32(1)
	remaining := int64(1)
	client := &deploymentClient{
		list: func(_ context.Context, options metav1.ListOptions) (*appsv1.DeploymentList, error) {
			if options.Limit != 2 || options.Continue != "next-page" {
				t.Fatalf("ListOptions = %#v", options)
			}

			return &appsv1.DeploymentList{
				ListMeta: metav1.ListMeta{Continue: "last-page", RemainingItemCount: &remaining},
				Items: []appsv1.Deployment{{
					ObjectMeta: metav1.ObjectMeta{Name: "workers", Namespace: "queues"},
					Spec:       appsv1.DeploymentSpec{Replicas: &one},
					Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
				}},
			}, nil
		},
	}
	adapter, err := New("queues", client)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	page, err := adapter.List(context.Background(), 2, "next-page")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if page.Continue != "last-page" || page.Remaining != 1 {
		t.Fatalf("List() metadata = %#v", page)
	}
	if len(page.Items) != 1 || page.Items[0].Name != "workers" || page.Items[0].ReadyReplicas != 1 {
		t.Fatalf("List() items = %#v", page.Items)
	}
}

func TestAdapterRejectsUnboundedList(t *testing.T) {
	t.Parallel()

	client := &deploymentClient{
		list: func(context.Context, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			t.Fatal("unbounded list reached Kubernetes")

			return nil, nil
		},
	}
	adapter, err := New("queues", client)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, input := range []struct {
		limit int64
		token string
	}{
		{},
		{limit: MaxPageSize + 1},
		{limit: 1, token: strings.Repeat("x", MaxContinueTokenBytes+1)},
	} {
		if _, err := adapter.List(context.Background(), input.limit, input.token); !errors.Is(err, ErrInvalidPage) {
			t.Fatalf("List(%d, token length %d) error = %v, want ErrInvalidPage", input.limit, len(input.token), err)
		}
	}
}

func (client *deploymentClient) GetScale(
	ctx context.Context,
	name string,
	options metav1.GetOptions,
) (*autoscalingv1.Scale, error) {
	return client.getScale(ctx, name, options)
}

func (client *deploymentClient) UpdateScale(
	ctx context.Context,
	name string,
	scale *autoscalingv1.Scale,
	options metav1.UpdateOptions,
) (*autoscalingv1.Scale, error) {
	return client.updateScale(ctx, name, scale, options)
}

func TestAdapterGetsDeploymentStatus(t *testing.T) {
	t.Parallel()

	replicas := int32(5)
	client := &deploymentClient{
		get: func(_ context.Context, name string, _ metav1.GetOptions) (*appsv1.Deployment, error) {
			if name != "billing-workers" {
				t.Fatalf("name = %q, want billing-workers", name)
			}

			return &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  "queues",
					Generation: 12,
				},
				Spec: appsv1.DeploymentSpec{Replicas: &replicas},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration:  11,
					UpdatedReplicas:     4,
					ReadyReplicas:       3,
					AvailableReplicas:   3,
					UnavailableReplicas: 2,
				},
			}, nil
		},
	}
	adapter, err := New("queues", client)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	status, err := adapter.Get(context.Background(), "billing-workers")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	want := Status{
		Namespace:           "queues",
		Name:                "billing-workers",
		Generation:          12,
		ObservedGeneration:  11,
		DesiredReplicas:     5,
		UpdatedReplicas:     4,
		ReadyReplicas:       3,
		AvailableReplicas:   3,
		UnavailableReplicas: 2,
	}
	if status != want {
		t.Fatalf("Get() = %#v, want %#v", status, want)
	}
}

func TestAdapterScalesThroughDeploymentScaleSubresource(t *testing.T) {
	t.Parallel()

	client := &deploymentClient{
		getScale: func(_ context.Context, name string, _ metav1.GetOptions) (*autoscalingv1.Scale, error) {
			if name != "billing-workers" {
				t.Fatalf("GetScale() name = %q", name)
			}

			return &autoscalingv1.Scale{
				ObjectMeta: metav1.ObjectMeta{ResourceVersion: "17"},
				Spec:       autoscalingv1.ScaleSpec{Replicas: 3},
			}, nil
		},
		updateScale: func(_ context.Context, name string, scale *autoscalingv1.Scale, _ metav1.UpdateOptions) (*autoscalingv1.Scale, error) {
			if name != "billing-workers" || scale.Spec.Replicas != 5 || scale.ResourceVersion != "17" {
				t.Fatalf("UpdateScale(%q, %#v)", name, scale)
			}

			return &autoscalingv1.Scale{
				ObjectMeta: metav1.ObjectMeta{ResourceVersion: "18"},
				Spec:       autoscalingv1.ScaleSpec{Replicas: 5},
				Status:     autoscalingv1.ScaleStatus{Replicas: 3},
			}, nil
		},
	}
	adapter, err := New("queues", client)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := adapter.Scale(context.Background(), "billing-workers", 5)
	if err != nil {
		t.Fatalf("Scale() error = %v", err)
	}
	want := ScaleResult{
		Namespace:       "queues",
		Name:            "billing-workers",
		DesiredReplicas: 5,
		CurrentReplicas: 3,
		ResourceVersion: "18",
	}
	if result != want {
		t.Fatalf("Scale() = %#v, want %#v", result, want)
	}
}

func TestAdapterRejectsInvalidScaleRequest(t *testing.T) {
	t.Parallel()

	client := &deploymentClient{
		getScale: func(context.Context, string, metav1.GetOptions) (*autoscalingv1.Scale, error) {
			t.Fatal("invalid scale reached Kubernetes")

			return nil, nil
		},
	}
	adapter, err := New("queues", client)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, input := range []struct {
		name     string
		replicas uint32
	}{
		{name: "Not_A_Deployment", replicas: 1},
		{name: "workers", replicas: 10_001},
	} {
		if _, err := adapter.Scale(context.Background(), input.name, input.replicas); !errors.Is(err, ErrInvalidWorkload) {
			t.Fatalf("Scale(%q, %d) error = %v, want ErrInvalidWorkload", input.name, input.replicas, err)
		}
	}
}

func TestAdapterRejectsInvalidKubernetesResponses(t *testing.T) {
	t.Parallel()

	operations := map[string]func(*Adapter) error{
		"missing deployment": func(adapter *Adapter) error {
			_, err := adapter.Get(context.Background(), "workers")

			return err
		},
		"cross namespace deployment": func(adapter *Adapter) error {
			_, err := adapter.Get(context.Background(), "workers")

			return err
		},
		"missing deployment list": func(adapter *Adapter) error {
			_, err := adapter.List(context.Background(), 1, "")

			return err
		},
		"cross namespace list": func(adapter *Adapter) error {
			_, err := adapter.List(context.Background(), 1, "")

			return err
		},
		"missing current scale": func(adapter *Adapter) error {
			_, err := adapter.Scale(context.Background(), "workers", 1)

			return err
		},
		"missing updated scale": func(adapter *Adapter) error {
			_, err := adapter.Scale(context.Background(), "workers", 1)

			return err
		},
	}
	clients := map[string]*deploymentClient{
		"missing deployment": {
			get: func(context.Context, string, metav1.GetOptions) (*appsv1.Deployment, error) { return nil, nil },
		},
		"cross namespace deployment": {
			get: func(context.Context, string, metav1.GetOptions) (*appsv1.Deployment, error) {
				return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "workers", Namespace: "other"}}, nil
			},
		},
		"missing deployment list": {
			list: func(context.Context, metav1.ListOptions) (*appsv1.DeploymentList, error) { return nil, nil },
		},
		"cross namespace list": {
			list: func(context.Context, metav1.ListOptions) (*appsv1.DeploymentList, error) {
				return &appsv1.DeploymentList{Items: []appsv1.Deployment{{
					ObjectMeta: metav1.ObjectMeta{Name: "workers", Namespace: "other"},
				}}}, nil
			},
		},
		"missing current scale": {
			getScale: func(context.Context, string, metav1.GetOptions) (*autoscalingv1.Scale, error) { return nil, nil },
		},
		"missing updated scale": {
			getScale: func(context.Context, string, metav1.GetOptions) (*autoscalingv1.Scale, error) {
				return &autoscalingv1.Scale{}, nil
			},
			updateScale: func(context.Context, string, *autoscalingv1.Scale, metav1.UpdateOptions) (*autoscalingv1.Scale, error) {
				return nil, nil
			},
		},
	}

	for name, operation := range operations {
		name, operation := name, operation
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			adapter, err := New("queues", clients[name])
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if err := operation(adapter); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("operation error = %v, want ErrInvalidResponse", err)
			}
		})
	}
}

func TestAdapterPropagatesKubernetesErrors(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("kubernetes unavailable")
	cases := map[string]struct {
		client    *deploymentClient
		operation func(*Adapter) error
	}{
		"get": {
			client: &deploymentClient{
				get: func(context.Context, string, metav1.GetOptions) (*appsv1.Deployment, error) {
					return nil, backendErr
				},
			},
			operation: func(adapter *Adapter) error {
				_, err := adapter.Get(context.Background(), "workers")

				return err
			},
		},
		"list": {
			client: &deploymentClient{
				list: func(context.Context, metav1.ListOptions) (*appsv1.DeploymentList, error) {
					return nil, backendErr
				},
			},
			operation: func(adapter *Adapter) error {
				_, err := adapter.List(context.Background(), 1, "")

				return err
			},
		},
		"get scale": {
			client: &deploymentClient{
				getScale: func(context.Context, string, metav1.GetOptions) (*autoscalingv1.Scale, error) {
					return nil, backendErr
				},
			},
			operation: func(adapter *Adapter) error {
				_, err := adapter.Scale(context.Background(), "workers", 1)

				return err
			},
		},
		"update scale": {
			client: &deploymentClient{
				getScale: func(context.Context, string, metav1.GetOptions) (*autoscalingv1.Scale, error) {
					return &autoscalingv1.Scale{}, nil
				},
				updateScale: func(context.Context, string, *autoscalingv1.Scale, metav1.UpdateOptions) (*autoscalingv1.Scale, error) {
					return nil, backendErr
				},
			},
			operation: func(adapter *Adapter) error {
				_, err := adapter.Scale(context.Background(), "workers", 1)

				return err
			},
		},
	}

	for name, test := range cases {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			adapter, err := New("queues", test.client)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if err := test.operation(adapter); !errors.Is(err, backendErr) {
				t.Fatalf("operation error = %v, want backend error", err)
			}
		})
	}
}
