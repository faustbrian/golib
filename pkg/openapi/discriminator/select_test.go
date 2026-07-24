package discriminator_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/discriminator"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func TestSelectAppliesExplicitImplicitAndDefaultMappings(t *testing.T) {
	t.Parallel()

	object := discriminatorObject(t, []jsonvalue.Member{
		{Name: "propertyName", Value: discriminatorString(t, "kind")},
		{Name: "mapping", Value: discriminatorObject(t, []jsonvalue.Member{
			{Name: "dog", Value: discriminatorString(t, "Dog")},
			{Name: "cat", Value: discriminatorString(t, "./Cat")},
		})},
		{Name: "defaultMapping", Value: discriminatorString(t, "Fallback")},
	})
	for _, test := range []struct {
		instance jsonvalue.Value
		target   string
		kind     discriminator.TargetKind
		source   discriminator.MatchKind
	}{
		{discriminatorInstance(t, "kind", "dog"), "Dog", discriminator.TargetSchemaName, discriminator.MatchExplicit},
		{discriminatorInstance(t, "kind", "cat"), "./Cat", discriminator.TargetURIReference, discriminator.MatchExplicit},
		{discriminatorInstance(t, "kind", "bird"), "bird", discriminator.TargetSchemaName, discriminator.MatchImplicit},
		{discriminatorObject(t, nil), "Fallback", discriminator.TargetSchemaName, discriminator.MatchDefault},
	} {
		selection, found, err := discriminator.Select(
			object, test.instance, discriminator.DefaultLimits(),
		)
		if err != nil {
			t.Fatal(err)
		}
		if !found || selection.Target != test.target ||
			selection.TargetKind != test.kind || selection.MatchKind != test.source {
			t.Fatalf("Select() = %#v, %t", selection, found)
		}
	}
}

func TestSelectRejectsMalformedAndBoundedInputs(t *testing.T) {
	t.Parallel()

	instance := discriminatorInstance(t, "kind", "dog")
	if _, _, err := discriminator.Select(
		jsonvalue.Null(), instance, discriminator.DefaultLimits(),
	); !errors.Is(err, discriminator.ErrInvalidDiscriminator) {
		t.Fatalf("non-object discriminator error = %v", err)
	}
	missingName := discriminatorObject(t, nil)
	if _, _, err := discriminator.Select(
		missingName, instance, discriminator.DefaultLimits(),
	); !errors.Is(err, discriminator.ErrInvalidDiscriminator) {
		t.Fatalf("missing property name error = %v", err)
	}
	limits := discriminator.DefaultLimits()
	limits.MaxMappings = 1
	mapping := discriminatorObject(t, []jsonvalue.Member{
		{Name: "propertyName", Value: discriminatorString(t, "kind")},
		{Name: "mapping", Value: discriminatorObject(t, []jsonvalue.Member{
			{Name: "dog", Value: discriminatorString(t, "Dog")},
			{Name: "cat", Value: discriminatorString(t, "Cat")},
		})},
	})
	if _, _, err := discriminator.Select(
		mapping, instance, limits,
	); !errors.Is(err, discriminator.ErrLimitExceeded) {
		t.Fatalf("mapping limit error = %v", err)
	}
}

func TestSelectRejectsEveryMalformedDiscriminatorField(t *testing.T) {
	t.Parallel()

	validInstance := discriminatorInstance(t, "kind", "dog")
	for _, test := range []struct {
		name          string
		discriminator jsonvalue.Value
		instance      jsonvalue.Value
		limits        discriminator.Limits
	}{
		{name: "instance", discriminator: discriminatorObject(t, []jsonvalue.Member{
			{Name: "propertyName", Value: discriminatorString(t, "kind")},
		}), instance: jsonvalue.Null(), limits: discriminator.DefaultLimits()},
		{name: "limits", discriminator: discriminatorObject(t, []jsonvalue.Member{
			{Name: "propertyName", Value: discriminatorString(t, "kind")},
		}), instance: validInstance, limits: discriminator.Limits{}},
		{name: "property type", discriminator: discriminatorObject(t, []jsonvalue.Member{
			{Name: "propertyName", Value: jsonvalue.Boolean(true)},
		}), instance: validInstance, limits: discriminator.DefaultLimits()},
		{name: "empty property", discriminator: discriminatorObject(t, []jsonvalue.Member{
			{Name: "propertyName", Value: discriminatorString(t, "")},
		}), instance: validInstance, limits: discriminator.DefaultLimits()},
		{name: "mapping type", discriminator: discriminatorObject(t, []jsonvalue.Member{
			{Name: "propertyName", Value: discriminatorString(t, "kind")},
			{Name: "mapping", Value: jsonvalue.Null()},
		}), instance: validInstance, limits: discriminator.DefaultLimits()},
		{name: "default type", discriminator: discriminatorObject(t, []jsonvalue.Member{
			{Name: "propertyName", Value: discriminatorString(t, "kind")},
			{Name: "defaultMapping", Value: jsonvalue.Boolean(true)},
		}), instance: discriminatorObject(t, nil), limits: discriminator.DefaultLimits()},
		{name: "empty default", discriminator: discriminatorObject(t, []jsonvalue.Member{
			{Name: "propertyName", Value: discriminatorString(t, "kind")},
			{Name: "defaultMapping", Value: discriminatorString(t, "")},
		}), instance: discriminatorObject(t, nil), limits: discriminator.DefaultLimits()},
		{name: "instance value type", discriminator: discriminatorObject(t, []jsonvalue.Member{
			{Name: "propertyName", Value: discriminatorString(t, "kind")},
		}), instance: discriminatorObject(t, []jsonvalue.Member{
			{Name: "kind", Value: jsonvalue.Boolean(true)},
		}), limits: discriminator.DefaultLimits()},
		{name: "mapping target type", discriminator: discriminatorObject(t, []jsonvalue.Member{
			{Name: "propertyName", Value: discriminatorString(t, "kind")},
			{Name: "mapping", Value: discriminatorObject(t, []jsonvalue.Member{
				{Name: "dog", Value: jsonvalue.Boolean(true)},
			})},
		}), instance: validInstance, limits: discriminator.DefaultLimits()},
		{name: "empty mapping target", discriminator: discriminatorObject(t, []jsonvalue.Member{
			{Name: "propertyName", Value: discriminatorString(t, "kind")},
			{Name: "mapping", Value: discriminatorObject(t, []jsonvalue.Member{
				{Name: "dog", Value: discriminatorString(t, "")},
			})},
		}), instance: validInstance, limits: discriminator.DefaultLimits()},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := discriminator.Select(
				test.discriminator, test.instance, test.limits,
			)
			if !errors.Is(err, discriminator.ErrInvalidDiscriminator) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSelectReturnsNoHintWithoutValueOrDefault(t *testing.T) {
	t.Parallel()

	value := discriminatorObject(t, []jsonvalue.Member{
		{Name: "propertyName", Value: discriminatorString(t, "kind")},
	})
	selection, found, err := discriminator.Select(
		value, discriminatorObject(t, nil), discriminator.DefaultLimits(),
	)
	if err != nil || found || selection != (discriminator.Selection{}) {
		t.Fatalf("Select() = %#v, %t, %v", selection, found, err)
	}
}

func TestSelectAcceptsTheExactMappingLimit(t *testing.T) {
	t.Parallel()

	value := discriminatorObject(t, []jsonvalue.Member{
		{Name: "propertyName", Value: discriminatorString(t, "kind")},
		{Name: "mapping", Value: discriminatorObject(t, []jsonvalue.Member{
			{Name: "dog", Value: discriminatorString(t, "Dog")},
		})},
	})
	selection, found, err := discriminator.Select(
		value,
		discriminatorInstance(t, "kind", "dog"),
		discriminator.Limits{MaxMappings: 1},
	)
	if err != nil || !found || selection.Target != "Dog" {
		t.Fatalf("Select() = %#v, %t, %v", selection, found, err)
	}
}

func discriminatorInstance(t *testing.T, name string, raw string) jsonvalue.Value {
	t.Helper()
	return discriminatorObject(t, []jsonvalue.Member{
		{Name: name, Value: discriminatorString(t, raw)},
	})
}

func discriminatorObject(t *testing.T, members []jsonvalue.Member) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Object(members)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func discriminatorString(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.String(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
