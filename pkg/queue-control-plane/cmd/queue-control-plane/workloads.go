package main

import (
	"io"
	"os"

	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
	kubernetesclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type workloadFileOpener func(string) (io.ReadCloser, error)
type deploymentFactoryProvider func() (controlkubernetes.DeploymentFactory, error)
type scaleDispatcherFactory func(controlkubernetes.TenantResolver) (control.Dispatcher, error)

func loadWorkloadRuntime(
	path string,
	maxBytes int64,
	open workloadFileOpener,
	newFactory deploymentFactoryProvider,
	newScale scaleDispatcherFactory,
) (workloadRuntime, error) {
	file, err := open(path)
	if err != nil {
		return workloadRuntime{}, err
	}
	defer func() { _ = file.Close() }()

	factory, err := newFactory()
	if err != nil {
		return workloadRuntime{}, err
	}
	resolver, err := controlkubernetes.LoadTenantResolver(file, maxBytes, factory)
	if err != nil {
		return workloadRuntime{}, err
	}
	dispatcher, err := newScale(resolver)
	if err != nil {
		return workloadRuntime{}, err
	}

	return workloadRuntime{Source: resolver, Dispatcher: dispatcher}, nil
}

func loadProductionWorkloads(path string, maxBytes int64) (workloadRuntime, error) {
	return loadWorkloadRuntime(
		path,
		maxBytes,
		openWorkloadFile,
		productionDeploymentFactory,
		newProductionScaleDispatcher,
	)
}

func openWorkloadFile(path string) (io.ReadCloser, error) {
	return os.Open(path) //nolint:gosec // The operator explicitly configures this bounded file path.
}

func productionDeploymentFactory() (controlkubernetes.DeploymentFactory, error) {
	return newDeploymentFactory(rest.InClusterConfig, newProductionKubernetesClient)
}

func newProductionKubernetesClient(config *rest.Config) (kubernetesclient.Interface, error) {
	return kubernetesclient.NewForConfig(config)
}

func newProductionScaleDispatcher(
	resolver controlkubernetes.TenantResolver,
) (control.Dispatcher, error) {
	return controlkubernetes.NewScaleDispatcher(resolver)
}

func newDeploymentFactory(
	loadConfig func() (*rest.Config, error),
	newClient func(*rest.Config) (kubernetesclient.Interface, error),
) (controlkubernetes.DeploymentFactory, error) {
	config, err := loadConfig()
	if err != nil {
		return nil, err
	}
	client, err := newClient(config)
	if err != nil {
		return nil, err
	}
	if missingDependency(client) {
		return nil, ErrInvalidWorkloadRuntime
	}

	return func(namespace string) controlkubernetes.DeploymentClient {
		return client.AppsV1().Deployments(namespace)
	}, nil
}
