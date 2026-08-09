//go:build integration && podintegration

package queueservice

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestKubernetesPodTerminationDuplicateWindowAndRecovery(t *testing.T) {
	contextName := requiredPodIntegrationEnvironment(t, "QUEUE_SERVICE_POD_CONTEXT")
	if !strings.HasPrefix(contextName, "kind-queueservice-") {
		t.Fatalf("QUEUE_SERVICE_POD_CONTEXT %q is not an isolated queueservice Kind context", contextName)
	}
	workerImage := requiredPodIntegrationEnvironment(t, "QUEUE_SERVICE_POD_IMAGE")
	redisImage := requiredPodIntegrationEnvironment(t, "QUEUE_SERVICE_POD_REDIS_IMAGE")
	valkeyImage := requiredPodIntegrationEnvironment(t, "QUEUE_SERVICE_POD_VALKEY_IMAGE")
	namespace := fmt.Sprintf("queueservice-e2e-%d", os.Getpid())
	kubectl := podKubectl{contextName: contextName, namespace: namespace}
	if output, err := kubectl.run("create", "namespace", namespace); err != nil {
		t.Fatalf("create pod-test namespace: %v: %s", err, output)
	}
	t.Cleanup(func() {
		_, _ = kubectl.run("delete", "namespace", namespace, "--wait=false")
	})

	backends := []struct {
		name    string
		pod     string
		image   string
		address string
		client  string
	}{
		{name: "redis-streams", pod: "redis", image: redisImage, address: "redis:6379", client: "redis-cli"},
		{name: "valkey-streams", pod: "valkey", image: valkeyImage, address: "valkey:6379", client: "valkey-cli"},
	}
	for _, backend := range backends {
		if output, err := kubectl.run(
			"run", backend.pod,
			"--restart=Never",
			"--image="+backend.image,
			"--image-pull-policy=IfNotPresent",
			"--port=6379",
		); err != nil {
			t.Fatalf("start %s pod: %v: %s", backend.name, err, output)
		}
		if output, err := kubectl.run(
			"expose", "pod", backend.pod,
			"--name="+backend.pod,
			"--port=6379",
		); err != nil {
			t.Fatalf("expose %s pod: %v: %s", backend.name, err, output)
		}
		if output, err := kubectl.run(
			"wait", "pod/"+backend.pod,
			"--for=condition=Ready",
			"--timeout=90s",
		); err != nil {
			t.Fatalf("wait for %s pod: %v: %s", backend.name, err, output)
		}
	}

	terminationPoints := []struct {
		mode        string
		phase       string
		wantPending int64
		wantEffects int64
	}{
		{mode: "before-effect", phase: "before-effect", wantPending: 1, wantEffects: 1},
		{mode: "after-effect", phase: "after-effect", wantPending: 1, wantEffects: 2},
		{mode: "after-settlement", phase: "after-settlement", wantPending: 0, wantEffects: 1},
	}
	for backendIndex, backend := range backends {
		for pointIndex, point := range terminationPoints {
			t.Run(backend.name+"/"+point.mode, func(t *testing.T) {
				slug := fmt.Sprintf("p%d-%d", backendIndex, pointIndex)
				stream := slug + "-jobs"
				group := slug + "-workers"
				effects := slug + "-effects"
				publisher := "publisher-" + slug
				kubectl.runDurableHelperPod(
					t, publisher, workerImage, backend.name, backend.address,
					stream, group, effects, "publish",
				)
				kubectl.awaitPodSucceeded(t, publisher)

				worker := "worker-" + slug
				kubectl.runDurableHelperPod(
					t, worker, workerImage, backend.name, backend.address,
					stream, group, effects, point.mode,
				)
				kubectl.awaitPodLog(t, worker, point.phase)
				if output, err := kubectl.run(
					"delete", "pod/"+worker,
					"--force", "--grace-period=0", "--wait=true",
				); err != nil {
					t.Fatalf("kill worker pod: %v: %s", err, output)
				}
				kubectl.awaitPendingCount(
					t, backend.pod, backend.client, stream, group, point.wantPending,
				)
				if point.wantPending > 0 {
					kubectl.awaitPendingIdle(
						t, backend.pod, backend.client, stream, group, durableProcessLease,
					)
				}

				recoveryA := "recovery-a-" + slug
				recoveryB := "recovery-b-" + slug
				kubectl.runDurableHelperPod(
					t, recoveryA, workerImage, backend.name, backend.address,
					stream, group, effects, "recover",
				)
				kubectl.runDurableHelperPod(
					t, recoveryB, workerImage, backend.name, backend.address,
					stream, group, effects, "recover",
				)
				kubectl.awaitPodSucceeded(t, recoveryA)
				kubectl.awaitPodSucceeded(t, recoveryB)
				kubectl.awaitPendingCount(t, backend.pod, backend.client, stream, group, 0)
				value := kubectl.backendCommand(
					t, backend.pod, backend.client, "--raw", "GET", effects,
				)
				effectCount, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
				if err != nil || effectCount != point.wantEffects {
					t.Fatalf(
						"pod handler effects = %q (%v), want %d",
						value,
						err,
						point.wantEffects,
					)
				}
			})
		}
	}
}

type podKubectl struct {
	contextName string
	namespace   string
}

func (kubectl podKubectl) run(arguments ...string) (string, error) {
	base := []string{"--context", kubectl.contextName}
	if kubectl.namespace != "" && arguments[0] != "create" && arguments[1] != "namespace" {
		base = append(base, "--namespace", kubectl.namespace)
	}
	command := exec.Command("kubectl", append(base, arguments...)...)
	output, err := command.CombinedOutput()

	return string(output), err
}

func (kubectl podKubectl) runDurableHelperPod(
	t *testing.T,
	name string,
	image string,
	backend string,
	address string,
	stream string,
	group string,
	effects string,
	mode string,
) {
	t.Helper()
	output, err := kubectl.run(
		"run", name,
		"--restart=Never",
		"--image="+image,
		"--image-pull-policy=Never",
		"--env="+durableBackendProcessHelper+"="+mode,
		"--env=QUEUE_SERVICE_DURABLE_BACKEND="+backend,
		"--env=QUEUE_SERVICE_DURABLE_ADDRESS="+address,
		"--env=QUEUE_SERVICE_DURABLE_STREAM="+stream,
		"--env=QUEUE_SERVICE_DURABLE_GROUP="+group,
		"--env=QUEUE_SERVICE_DURABLE_EFFECTS="+effects,
		"--",
		"-test.run=^TestDurableBackendProcessHelper$",
	)
	if err != nil {
		t.Fatalf("start durable helper pod %s: %v: %s", name, err, output)
	}
}

func (kubectl podKubectl) awaitPodSucceeded(t *testing.T, name string) {
	t.Helper()
	output, err := kubectl.run(
		"wait", "pod/"+name,
		"--for=jsonpath={.status.phase}=Succeeded",
		"--timeout=30s",
	)
	if err != nil {
		logs, _ := kubectl.run("logs", name)
		t.Fatalf("wait for helper pod %s: %v: %s: %s", name, err, output, logs)
	}
}

func (kubectl podKubectl) awaitPodLog(t *testing.T, name, wanted string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		logs, _ := kubectl.run("logs", name)
		for _, line := range strings.Split(logs, "\n") {
			if strings.TrimSpace(line) == wanted {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("pod %s logs did not contain %q: %s", name, wanted, logs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (kubectl podKubectl) backendCommand(
	t *testing.T,
	pod string,
	client string,
	arguments ...string,
) string {
	t.Helper()
	command := append([]string{"exec", pod, "--", client}, arguments...)
	output, err := kubectl.run(command...)
	if err != nil {
		t.Fatalf("inspect durable backend pod: %v: %s", err, output)
	}

	return output
}

func (kubectl podKubectl) awaitPendingCount(
	t *testing.T,
	pod string,
	client string,
	stream string,
	group string,
	wanted int64,
) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		output := kubectl.backendCommand(
			t, pod, client, "--raw", "XPENDING", stream, group,
		)
		lines := strings.Split(strings.TrimSpace(output), "\n")
		count, err := strconv.ParseInt(lines[0], 10, 64)
		if err == nil && count == wanted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pod pending count = %q (%v), want %d", output, err, wanted)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (kubectl podKubectl) awaitPendingIdle(
	t *testing.T,
	pod string,
	client string,
	stream string,
	group string,
	minimum time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		output := kubectl.backendCommand(
			t, pod, client, "--raw", "XPENDING", stream, group, "-", "+", "1",
		)
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) >= 3 {
			idle, err := strconv.ParseInt(lines[2], 10, 64)
			if err == nil && time.Duration(idle)*time.Millisecond >= minimum {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("pod pending lease = %q, want idle >= %s", output, minimum)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func requiredPodIntegrationEnvironment(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required for pod integration", name)
	}

	return value
}
