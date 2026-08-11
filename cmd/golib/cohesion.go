package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var cohesionFamilyIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type cohesionConfig struct {
	SchemaVersion int              `json:"schema_version"`
	Families      []cohesionFamily `json:"families"`
}

type cohesionFamily struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Modules     []string `json:"modules"`
}

func loadCohesionConfig(root string) (cohesionConfig, error) {
	path := filepath.Join(root, ".golib", "cohesion.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return cohesionConfig{}, fmt.Errorf("read cohesion metadata: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	current := cohesionConfig{}
	if err := decoder.Decode(&current); err != nil {
		return cohesionConfig{}, fmt.Errorf("decode cohesion metadata: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return cohesionConfig{}, errors.New("decode cohesion metadata: trailing JSON value")
	}
	return current, nil
}

func applyCohesionFamilies(current *catalog, config cohesionConfig) error {
	if config.SchemaVersion != 1 {
		return fmt.Errorf("cohesion metadata schema = %d, want 1", config.SchemaVersion)
	}
	modules := make(map[string]int, len(current.Modules))
	for index, item := range current.Modules {
		modules[item.Directory] = index
	}
	seenFamilies := make(map[string]bool, len(config.Families))
	seenModules := make(map[string]string)
	for familyOrder, family := range config.Families {
		if !cohesionFamilyIDPattern.MatchString(family.ID) {
			return fmt.Errorf("invalid cohesion family identifier %q", family.ID)
		}
		if seenFamilies[family.ID] {
			return fmt.Errorf("duplicate cohesion family %q", family.ID)
		}
		seenFamilies[family.ID] = true
		if strings.TrimSpace(family.Label) == "" {
			return fmt.Errorf("cohesion family %s has no label", family.ID)
		}
		if strings.TrimSpace(family.Description) == "" {
			return fmt.Errorf("cohesion family %s has no description", family.ID)
		}
		if len(family.Modules) == 0 {
			return fmt.Errorf("cohesion family %s has no modules", family.ID)
		}
		if !slices.IsSorted(family.Modules) {
			return fmt.Errorf("cohesion family %s modules are not sorted", family.ID)
		}
		for _, directory := range family.Modules {
			index, exists := modules[directory]
			if !exists {
				return fmt.Errorf("cohesion family %s references unknown module %s", family.ID, directory)
			}
			if !current.Modules[index].Releasable {
				return fmt.Errorf("cohesion family %s references non-releasable module %s", family.ID, directory)
			}
			if previous, duplicate := seenModules[directory]; duplicate {
				return fmt.Errorf("module %s belongs to cohesion families %s and %s", directory, previous, family.ID)
			}
			seenModules[directory] = family.ID
			current.Modules[index].Family = family.ID
			current.Modules[index].FamilyLabel = family.Label
			current.Modules[index].FamilyDescription = family.Description
			current.Modules[index].FamilyOrder = familyOrder + 1
		}
	}
	for _, item := range current.Modules {
		if item.Releasable && seenModules[item.Directory] == "" {
			return fmt.Errorf("releasable module %s has no cohesion family", item.Directory)
		}
	}
	return nil
}

func validateCohesionContract(root string, current catalog) error {
	familyOrders := map[int]string{}
	familyLabels := map[string]string{}
	for _, item := range current.Modules {
		if !item.Releasable {
			if item.Family != "" || item.FamilyLabel != "" ||
				item.FamilyDescription != "" || item.FamilyOrder != 0 {
				return fmt.Errorf("non-releasable module %s has cohesion family metadata", item.Directory)
			}
			continue
		}
		if item.Family == "" || item.FamilyLabel == "" ||
			item.FamilyDescription == "" || item.FamilyOrder <= 0 {
			return fmt.Errorf("releasable module %s has incomplete family metadata", item.Directory)
		}
		if previous := familyOrders[item.FamilyOrder]; previous != "" && previous != item.Family {
			return fmt.Errorf("cohesion family order %d is shared by %s and %s", item.FamilyOrder, previous, item.Family)
		}
		familyOrders[item.FamilyOrder] = item.Family
		identity := item.FamilyLabel + "\x00" + item.FamilyDescription
		if previous := familyLabels[item.Family]; previous != "" && previous != identity {
			return fmt.Errorf("cohesion family %s has conflicting labels or descriptions", item.Family)
		}
		familyLabels[item.Family] = identity

		for _, name := range []string{"README.md", "CHANGELOG.md", "LICENSE"} {
			if err := validateCohesionEntryPoint(filepath.Join(root, item.Directory, name)); err != nil {
				return fmt.Errorf("module %s %s: %w", item.Directory, name, err)
			}
		}
		hasPublicPackage := false
		for _, packageInfo := range item.Packages {
			if packageInfo.Production && packageInfo.Kind == "public" {
				hasPublicPackage = true
				break
			}
		}
		if !hasPublicPackage {
			return fmt.Errorf("releasable module %s has no public production package", item.Directory)
		}
	}
	return nil
}

func validateCohesionEntryPoint(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("must be a nonempty regular file")
	}
	return nil
}
