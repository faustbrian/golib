package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
	kubernetesclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestLoadWorkloadRuntimeBuildsResolverAndScaleDispatcher(t *testing.T) {
	t.Parallel()

	document := `{"tenants":[{"id":"tenant-1","namespace":"workers"}]}`
	runtime, err := loadWorkloadRuntime(
		"/etc/control/tenants.json",
		1024,
		func(string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(document)), nil
		},
		func() (controlkubernetes.DeploymentFactory, error) {
			client := fake.NewClientset()

			return func(namespace string) controlkubernetes.DeploymentClient {
				return client.AppsV1().Deployments(namespace)
			}, nil
		},
		func(resolver controlkubernetes.TenantResolver) (control.Dispatcher, error) {
			return controlkubernetes.NewScaleDispatcher(resolver)
		},
	)
	if err != nil || runtime.Source == nil || runtime.Dispatcher == nil {
		t.Fatalf("loadWorkloadRuntime() = (%+v, %v), want complete runtime", runtime, err)
	}
}

func TestLoadWorkloadRuntimePropagatesEveryFailure(t *testing.T) {
	t.Parallel()

	stageErr := errors.New("stage failed")
	document := `{"tenants":[{"id":"tenant-1","namespace":"workers"}]}`
	validOpen := func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(document)), nil
	}
	validFactory := func() (controlkubernetes.DeploymentFactory, error) {
		client := fake.NewClientset()

		return func(namespace string) controlkubernetes.DeploymentClient {
			return client.AppsV1().Deployments(namespace)
		}, nil
	}
	validScale := func(resolver controlkubernetes.TenantResolver) (control.Dispatcher, error) {
		return controlkubernetes.NewScaleDispatcher(resolver)
	}
	tests := map[string]struct {
		open    workloadFileOpener
		factory deploymentFactoryProvider
		scale   scaleDispatcherFactory
		want    error
	}{
		"open": {
			open: func(string) (io.ReadCloser, error) { return nil, stageErr },
			want: stageErr,
		},
		"client": {
			open: validOpen,
			factory: func() (controlkubernetes.DeploymentFactory, error) {
				return nil, stageErr
			},
			want: stageErr,
		},
		"document": {
			open: func(string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(`{}`)), nil
			},
			factory: validFactory,
			scale:   validScale,
			want:    controlkubernetes.ErrInvalidTenantDocument,
		},
		"scale": {
			open:    validOpen,
			factory: validFactory,
			scale: func(controlkubernetes.TenantResolver) (control.Dispatcher, error) {
				return nil, stageErr
			},
			want: stageErr,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runtime, err := loadWorkloadRuntime("tenants.json", 1024, test.open, test.factory, test.scale)
			if runtime != (workloadRuntime{}) || !errors.Is(err, test.want) {
				t.Fatalf("loadWorkloadRuntime() = (%+v, %v), want zero and %v", runtime, err, test.want)
			}
		})
	}
}

func TestNewDeploymentFactoryPropagatesConfigAndClientFailures(t *testing.T) {
	t.Parallel()

	stageErr := errors.New("stage failed")
	if factory, err := newDeploymentFactory(
		func() (*rest.Config, error) { return nil, stageErr },
		func(config *rest.Config) (kubernetesclient.Interface, error) {
			return kubernetesclient.NewForConfig(config)
		},
	); factory != nil || !errors.Is(err, stageErr) {
		t.Fatalf("newDeploymentFactory(config) = (%v, %v)", factory, err)
	}
	if factory, err := newDeploymentFactory(
		func() (*rest.Config, error) { return &rest.Config{}, nil },
		func(*rest.Config) (kubernetesclient.Interface, error) { return nil, nil },
	); factory != nil || !errors.Is(err, ErrInvalidWorkloadRuntime) {
		t.Fatalf("newDeploymentFactory(nil client) = (%v, %v)", factory, err)
	}
	if factory, err := newDeploymentFactory(
		func() (*rest.Config, error) { return &rest.Config{}, nil },
		func(*rest.Config) (kubernetesclient.Interface, error) { return nil, stageErr },
	); factory != nil || !errors.Is(err, stageErr) {
		t.Fatalf("newDeploymentFactory(client) = (%v, %v)", factory, err)
	}

	client := fake.NewClientset()
	factory, err := newDeploymentFactory(
		func() (*rest.Config, error) { return &rest.Config{}, nil },
		func(*rest.Config) (kubernetesclient.Interface, error) { return client, nil },
	)
	if err != nil || factory("workers") == nil {
		t.Fatalf("newDeploymentFactory() = (%v, %v), want factory and nil", factory, err)
	}
}

func TestProductionWorkloadConstructorsAreServiceIndependent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tenants.json")
	document := `{"tenants":[{"id":"tenant-1","namespace":"workers"}]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	file, err := openWorkloadFile(path)
	if err != nil {
		t.Fatalf("openWorkloadFile() error = %v", err)
	}
	_ = file.Close()

	factory, factoryErr := productionDeploymentFactory()
	if factory == nil && factoryErr == nil {
		t.Fatal("productionDeploymentFactory() returned no factory or error")
	}
	client, err := newProductionKubernetesClient(&rest.Config{Host: "http://127.0.0.1"})
	if err != nil || client == nil {
		t.Fatalf("newProductionKubernetesClient() = (%v, %v)", client, err)
	}
	resolver, err := controlkubernetes.LoadTenantResolver(
		strings.NewReader(document),
		1024,
		func(namespace string) controlkubernetes.DeploymentClient {
			return client.AppsV1().Deployments(namespace)
		},
	)
	if err != nil {
		t.Fatalf("NewStaticTenantResolver() error = %v", err)
	}
	dispatcher, err := newProductionScaleDispatcher(resolver)
	if err != nil || dispatcher == nil {
		t.Fatalf("newProductionScaleDispatcher() = (%v, %v)", dispatcher, err)
	}

	runtime, err := loadProductionWorkloads(path, 1024)
	if err == nil && (runtime.Source == nil || runtime.Dispatcher == nil) {
		t.Fatalf("loadProductionWorkloads() = (%+v, nil), want complete runtime", runtime)
	}
}
