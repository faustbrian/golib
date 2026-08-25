package main

import (
	"reflect"
	"slices"
	"testing"
)

func TestBuildStandaloneManifestMapsFamiliesAndReleaseDependencies(t *testing.T) {
	t.Parallel()

	current := catalog{
		Modules: []module{
			{Directory: ".", Path: canonicalRoot, Releasable: false},
			{
				Directory:  "pkg/postgres",
				Path:       canonicalRoot + "/pkg/postgres",
				Releasable: true,
			},
			{
				Directory:         "pkg/outbox",
				Path:              canonicalRoot + "/pkg/outbox",
				Releasable:        true,
				OwnedDependencies: []string{canonicalRoot + "/pkg/postgres"},
			},
			{
				Directory:         "pkg/outbox/adapters/gokafka",
				Path:              canonicalRoot + "/pkg/outbox/adapters/gokafka",
				Releasable:        true,
				OwnedDependencies: []string{canonicalRoot + "/pkg/outbox"},
			},
			{
				Directory:  "pkg/rabbitstream",
				Path:       canonicalRoot + "/pkg/rabbitstream",
				Releasable: true,
			},
			{
				Directory:  "pkg/secret-store/adapters/awssecretsmanager",
				Path:       canonicalRoot + "/pkg/secret-store/adapters/awssecretsmanager",
				Releasable: true,
			},
		},
	}

	manifest, err := buildStandaloneManifest(current, "source-commit", 32743496559)
	if err != nil {
		t.Fatalf("buildStandaloneManifest() error = %v", err)
	}

	if manifest.Source.Commit != "source-commit" || manifest.Source.CIRun != 32743496559 {
		t.Fatalf("source = %#v", manifest.Source)
	}
	if got := repositoryNames(manifest.Repositories); !slices.Equal(
		got,
		[]string{"go-postgres", "go-rabbitmq-streams", "go-transactional-outbox"},
	) {
		t.Fatalf("repository names = %v", got)
	}
	if got := legacyRepositoryNames(manifest.Repositories); !slices.Equal(
		got,
		[]string{"go-postgresql", "go-rabbitmq-streams", "go-outbox"},
	) {
		t.Fatalf("legacy repository names = %v", got)
	}
	if got := standaloneModulePaths(manifest.Modules); !slices.Equal(got, []string{
		"github.com/faustbrian/go-postgres",
		"github.com/faustbrian/go-rabbitmq-streams",
		"github.com/faustbrian/go-transactional-outbox",
		"github.com/faustbrian/go-transactional-outbox/adapters/gokafka",
	}) {
		t.Fatalf("module paths = %v", got)
	}
	if !reflect.DeepEqual(manifest.ReleaseWaves, [][]string{
		{
			"github.com/faustbrian/go-postgres",
			"github.com/faustbrian/go-rabbitmq-streams",
		},
		{"github.com/faustbrian/go-transactional-outbox"},
		{"github.com/faustbrian/go-transactional-outbox/adapters/gokafka"},
	}) {
		t.Fatalf("release waves = %v", manifest.ReleaseWaves)
	}
	if !slices.Equal(manifest.ExcludedFamilies, []string{"secret-store"}) {
		t.Fatalf("excluded families = %v", manifest.ExcludedFamilies)
	}
	for _, item := range manifest.Modules {
		if item.Path == "github.com/faustbrian/go-postgres" && item.ReleaseTag != "v1.0.1" {
			t.Fatalf("postgres release tag = %q", item.ReleaseTag)
		}
	}
}

func TestBuildStandaloneManifestRejectsUnknownOwnedDependency(t *testing.T) {
	t.Parallel()

	current := catalog{Modules: []module{{
		Directory:         "pkg/example",
		Path:              canonicalRoot + "/pkg/example",
		Releasable:        true,
		OwnedDependencies: []string{canonicalRoot + "/pkg/missing"},
	}}}

	if _, err := buildStandaloneManifest(current, "source-commit", 1); err == nil {
		t.Fatal("buildStandaloneManifest() accepted an unknown owned dependency")
	}
}

func repositoryNames(repositories []standaloneRepository) []string {
	names := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		names = append(names, repository.Name)
	}

	return names
}

func legacyRepositoryNames(repositories []standaloneRepository) []string {
	names := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		names = append(names, repository.LegacyName)
	}

	return names
}

func standaloneModulePaths(modules []standaloneModulePlan) []string {
	paths := make([]string, 0, len(modules))
	for _, item := range modules {
		paths = append(paths, item.Path)
	}

	return paths
}
