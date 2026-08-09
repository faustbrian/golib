package workflow

import (
	"fmt"
)

type definitionKey struct {
	name    string
	version string
}

type migrationKey struct {
	name string
	from string
	to   string
}

// MigrationState is opaque application-owned persisted state passed through an
// explicit version migration. Registry callers must persist the migrated state
// and target version atomically.
type MigrationState struct {
	Data []byte
}

// Migration declares one explicit directed version edge.
type Migration struct {
	Name        string
	FromVersion string
	ToVersion   string
	Apply       func(MigrationState) (MigrationState, error)
}

// Registry is an immutable explicit definition and migration registry.
type Registry struct {
	definitions map[definitionKey]Definition
	migrations  map[migrationKey]Migration
}

// CompileDefinitions constructs a registry without migration edges.
func CompileDefinitions(definitions ...Definition) (*Registry, error) {
	return CompileRegistry(definitions, nil)
}

// CompileRegistry validates all immutable definitions and explicit migrations.
func CompileRegistry(definitions []Definition, migrations []Migration) (*Registry, error) {
	registry := &Registry{
		definitions: make(map[definitionKey]Definition, len(definitions)),
		migrations:  make(map[migrationKey]Migration, len(migrations)),
	}
	for _, definition := range definitions {
		if definition.fingerprint == "" {
			return nil, invalidDefinition("compiled definition")
		}
		key := definitionKey{name: definition.Name(), version: definition.Version()}
		if _, exists := registry.definitions[key]; exists {
			return nil, fmt.Errorf("%w: %s@%s", ErrDuplicateDefinition, key.name, key.version)
		}
		registry.definitions[key] = definition
	}
	for _, definition := range definitions {
		for _, step := range definition.spec.Steps {
			if step.Kind != StepChild {
				continue
			}
			child, exists := registry.definitions[definitionKey{
				name: step.ChildDefinition.Name(), version: step.ChildDefinition.Version(),
			}]
			if !exists {
				return nil, fmt.Errorf("%w: child %s@%s", ErrDefinitionNotFound,
					step.ChildDefinition.Name(), step.ChildDefinition.Version())
			}
			if child.Reference() != step.ChildDefinition {
				return nil, fmt.Errorf("%w: child %s@%s", ErrDefinitionMismatch,
					step.ChildDefinition.Name(), step.ChildDefinition.Version())
			}
		}
	}

	for _, migration := range migrations {
		from := definitionKey{name: migration.Name, version: migration.FromVersion}
		to := definitionKey{name: migration.Name, version: migration.ToVersion}
		if migration.Apply == nil || migration.FromVersion == migration.ToVersion {
			return nil, fmt.Errorf("%w: incomplete edge", ErrInvalidMigration)
		}
		if _, exists := registry.definitions[from]; !exists {
			return nil, fmt.Errorf("%w: source definition", ErrInvalidMigration)
		}
		if _, exists := registry.definitions[to]; !exists {
			return nil, fmt.Errorf("%w: target definition", ErrInvalidMigration)
		}
		key := migrationKey{name: migration.Name, from: migration.FromVersion, to: migration.ToVersion}
		if _, exists := registry.migrations[key]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateMigration, migration.Name)
		}
		registry.migrations[key] = migration
	}

	return registry, nil
}

// Resolve returns one exact pinned immutable definition version.
func (registry *Registry) Resolve(name, version string) (Definition, error) {
	if registry == nil {
		return Definition{}, ErrDefinitionNotFound
	}
	definition, exists := registry.definitions[definitionKey{name: name, version: version}]
	if !exists {
		return Definition{}, ErrDefinitionNotFound
	}
	return definition, nil
}

// Migration returns one explicitly declared direct version edge.
func (registry *Registry) Migration(name, fromVersion, toVersion string) (Migration, error) {
	if registry == nil {
		return Migration{}, ErrMigrationNotFound
	}
	migration, exists := registry.migrations[migrationKey{name: name, from: fromVersion, to: toVersion}]
	if !exists {
		return Migration{}, ErrMigrationNotFound
	}
	return migration, nil
}
