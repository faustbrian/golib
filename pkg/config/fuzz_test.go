package config_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/decode"
	"github.com/faustbrian/golib/pkg/config/dotenv"
	"github.com/faustbrian/golib/pkg/config/environment"
	jsonsource "github.com/faustbrian/golib/pkg/config/json"
	tomlsource "github.com/faustbrian/golib/pkg/config/toml"
	yamlsource "github.com/faustbrian/golib/pkg/config/yaml"
)

func FuzzStructuredSources(f *testing.F) {
	f.Add([]byte(`{"name":"worker","port":8080}`))
	f.Add([]byte("name: worker\nport: 8080\n"))
	f.Add([]byte("name = \"worker\"\nport = 8080\n"))
	f.Add([]byte("{\x00\xff"))

	f.Fuzz(func(t *testing.T, data []byte) {
		sources := []struct {
			build func([]byte) (config.Source, error)
		}{
			{build: func(data []byte) (config.Source, error) {
				return jsonsource.Bytes(data, jsonsource.Options{Name: "fuzz-json"})
			}},
			{build: func(data []byte) (config.Source, error) {
				return yamlsource.Bytes(data, yamlsource.Options{Name: "fuzz-yaml"})
			}},
			{build: func(data []byte) (config.Source, error) {
				return tomlsource.Bytes(data, tomlsource.Options{Name: "fuzz-toml"})
			}},
		}
		for _, fixture := range sources {
			source, err := fixture.build(data)
			if err != nil {
				t.Fatalf("construct source: %v", err)
			}
			_, _ = source.Load(context.Background())
		}
	})
}

func FuzzDotenvInterpolation(f *testing.F) {
	f.Add("APP_NAME=${NAME:-worker}\n", "worker")
	f.Add("APP_NAME=${APP_NAME}\n", "")
	f.Add("APP_NAME='${NAME}'\n", "literal")
	f.Add("\x00=${MISSING}\n", "value")

	type settings struct {
		Name string `config:"name"`
	}
	f.Fuzz(func(t *testing.T, contents, external string) {
		source, err := dotenv.BytesFor[settings]([]byte(contents), dotenv.Options{
			Name: "fuzz-dotenv", Prefix: "APP_",
			Limits: dotenv.Limits{
				MaxBytes: 4096, MaxLines: 64, MaxLineBytes: 1024, MaxKeys: 64,
			},
			Interpolation: &dotenv.Interpolation{
				Variables: map[string]string{"NAME": external}, IncludeFile: true,
				MaxDepth: 8, MaxExpandedBytes: 4096,
			},
		})
		if err != nil {
			t.Fatalf("construct source: %v", err)
		}
		_, _ = source.Load(context.Background())
	})
}

func FuzzEnvironmentMapping(f *testing.F) {
	f.Add("APP_VALUE", "42")
	f.Add("app_value", "true")
	f.Add("APP_VALUE", "[1,2,3]")
	f.Add("BAD-NAME", "secret")

	type settings struct {
		Value int `config:"value"`
	}
	f.Fuzz(func(t *testing.T, name, value string) {
		if len(name)+len(value) > 4096 {
			t.Skip()
		}
		source, err := environment.EnvironFor[settings](
			[]string{name + "=" + value},
			environment.Options{
				Name: "fuzz-environment", Prefix: "APP_",
				Limits: environment.Limits{
					MaxVariables: 4, MaxBytes: 4096, MaxValueBytes: 2048,
				},
			},
		)
		if err != nil {
			t.Fatalf("construct source: %v", err)
		}
		_, _ = source.Load(context.Background())
	})
}

func FuzzDecodeTagsAndDestinationTypes(f *testing.F) {
	f.Add("value", uint8(0), "42")
	f.Add("value,required", uint8(1), "true")
	f.Add("-", uint8(2), "ignored")
	f.Add(",secret", uint8(3), "value")
	f.Add("value", uint8(4), "text-hook")
	f.Add("value", uint8(5), "panic:text-hook")
	f.Add("value", uint8(6), "value-hook")
	f.Add("value", uint8(7), "panic:value-hook")
	f.Add("value", uint8(8), "pointer")
	f.Add("value", uint8(9), "interface")
	f.Add("value", uint8(10), "recursive")
	f.Add("value", uint8(11), "context-text-hook")
	f.Add("value", uint8(12), "panic:context-value-hook")

	f.Fuzz(func(t *testing.T, tag string, selector uint8, input string) {
		if len(tag)+len(input) > 2048 {
			t.Skip()
		}
		var destination any
		switch selector % 13 {
		case 0, 1, 2, 3:
			types := []reflect.Type{
				reflect.TypeFor[int](),
				reflect.TypeFor[bool](),
				reflect.TypeFor[[]string](),
				reflect.TypeFor[map[string]string](),
			}
			typeOf := reflect.StructOf([]reflect.StructField{{
				Name: "Value", Type: types[int(selector)%len(types)],
				Tag: reflect.StructTag(`config:"` + tag + `"`),
			}})
			destination = reflect.New(typeOf).Interface()
		case 4, 5:
			destination = &fuzzTextHookSettings{}
		case 6, 7:
			destination = &fuzzValueHookSettings{}
		case 8:
			destination = &fuzzPointerSettings{}
		case 9:
			destination = &fuzzInterfaceSettings{}
		case 10:
			destination = &fuzzRecursiveSettings{}
		case 11:
			destination = &fuzzContextTextHookSettings{}
		case 12:
			destination = &fuzzContextValueHookSettings{}
		}
		_ = decode.Value(map[string]any{"value": input}, destination)
	})
}

type fuzzTextHookSettings struct {
	Value fuzzTextHook `config:"value"`
}

type fuzzTextHook string

func (value *fuzzTextHook) UnmarshalText(text []byte) error {
	input := string(text)
	if strings.HasPrefix(input, "panic:") {
		panic(input)
	}
	if strings.HasPrefix(input, "error:") {
		return errors.New(input)
	}
	*value = fuzzTextHook(input)
	return nil
}

type fuzzValueHookSettings struct {
	Value fuzzValueHook `config:"value"`
}

type fuzzValueHook struct{ Value string }

func (value *fuzzValueHook) UnmarshalConfigValue(input any) error {
	text, _ := input.(string)
	if strings.HasPrefix(text, "panic:") {
		panic(text)
	}
	if strings.HasPrefix(text, "error:") {
		return errors.New(text)
	}
	value.Value = text
	return nil
}

type fuzzPointerSettings struct {
	Value *string `config:"value"`
}

type fuzzInterfaceSettings struct {
	Value any `config:"value"`
}

type fuzzRecursiveSettings struct {
	Value *fuzzRecursiveSettings `config:"value"`
}

type fuzzContextTextHookSettings struct {
	Value fuzzContextTextHook `config:"value"`
}

type fuzzContextTextHook string

func (value *fuzzContextTextHook) UnmarshalTextContext(ctx context.Context, text []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input := string(text)
	if strings.HasPrefix(input, "panic:") {
		panic(input)
	}
	*value = fuzzContextTextHook(input)
	return nil
}

type fuzzContextValueHookSettings struct {
	Value fuzzContextValueHook `config:"value"`
}

type fuzzContextValueHook struct{ Value string }

func (value *fuzzContextValueHook) UnmarshalConfigValueContext(ctx context.Context, input any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	text, _ := input.(string)
	if strings.HasPrefix(text, "panic:") {
		panic(text)
	}
	value.Value = text
	return nil
}
