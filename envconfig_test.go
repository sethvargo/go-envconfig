// Copyright The envconfig Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package envconfig

import (
	"context"
	"encoding"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/google/go-cmp/cmp"
)

var _ Decoder = (*CustomDecoderType)(nil)

// CustomDecoderType is used to test custom decoding using Decoder.
type CustomDecoderType struct {
	value string
}

func (c *CustomDecoderType) EnvDecode(val string) error {
	c.value = "CUSTOM-" + val
	return nil
}

// Level mirrors Zap's level marshalling to reproduce an issue for tests.
type Level int8

const (
	LevelDebug Level = 0
	LevelInfo  Level = 5
	LevelError Level = 100
)

func (l *Level) UnmarshalText(text []byte) error {
	switch string(text) {
	case "debug":
		*l = LevelDebug
		return nil
	case "info", "": // default
		*l = LevelInfo
		return nil
	case "error":
		*l = LevelError
		return nil
	default:
		return fmt.Errorf("unknown level %s", string(text))
	}
}

var (
	_ encoding.BinaryUnmarshaler = (*CustomStdLibDecodingType)(nil)
	_ encoding.TextUnmarshaler   = (*CustomStdLibDecodingType)(nil)
	_ json.Unmarshaler           = (*CustomStdLibDecodingType)(nil)
	_ gob.GobDecoder             = (*CustomStdLibDecodingType)(nil)
)

// CustomStdLibDecodingType is used to test custom decoding using the standard
// library custom unmarshaling interfaces.
type CustomStdLibDecodingType struct {
	// used to control implementations
	implementsTextUnmarshaler   bool
	implementsBinaryUnmarshaler bool
	implementsJSONUnmarshaler   bool
	implementsGobDecoder        bool

	value string
}

// Equal returns whether the decoded values are equal.
func (c CustomStdLibDecodingType) Equal(c2 CustomStdLibDecodingType) bool {
	return c.value == c2.value
}

func (c *CustomStdLibDecodingType) UnmarshalBinary(data []byte) error {
	if !c.implementsBinaryUnmarshaler {
		return errors.New("binary unmarshaler not implemented")
	}
	c.value = "BINARY-" + string(data)
	return nil
}

func (c *CustomStdLibDecodingType) UnmarshalText(text []byte) error {
	if !c.implementsTextUnmarshaler {
		return errors.New("text unmarshaler not implemented")
	}
	c.value = "TEXT-" + string(text)
	return nil
}

func (c *CustomStdLibDecodingType) UnmarshalJSON(data []byte) error {
	if !c.implementsJSONUnmarshaler {
		return errors.New("JSON unmarshaler not implemented")
	}
	c.value = "JSON-" + string(data)
	return nil
}

func (c *CustomStdLibDecodingType) GobDecode(data []byte) error {
	if !c.implementsGobDecoder {
		return errors.New("Gob decoder not implemented")
	}
	c.value = "GOB-" + string(data)
	return nil
}

var (
	_ Decoder                    = (*CustomTypeError)(nil)
	_ encoding.BinaryUnmarshaler = (*CustomTypeError)(nil)
	_ gob.GobDecoder             = (*CustomTypeError)(nil)
	_ json.Unmarshaler           = (*CustomTypeError)(nil)
	_ encoding.TextUnmarshaler   = (*CustomTypeError)(nil)
)

// CustomTypeError returns a "broken" error via the custom decoder.
type CustomTypeError struct {
	Field string
}

func (c *CustomTypeError) EnvDecode(val string) error {
	return fmt.Errorf("broken")
}

func (c *CustomTypeError) UnmarshalBinary(data []byte) error {
	return fmt.Errorf("must never be returned")
}

func (c *CustomTypeError) GobDecode(data []byte) error {
	return fmt.Errorf("must never be returned")
}

func (c *CustomTypeError) UnmarshalJSON(data []byte) error {
	return fmt.Errorf("must never be returned")
}

func (c *CustomTypeError) UnmarshalText(text []byte) error {
	return fmt.Errorf("must never be returned")
}

// valueMutatorFunc is used for testing mutators.
var valueMutatorFunc MutatorFunc = func(ctx context.Context, oKey, rKey, oVal, rVal string) (string, bool, error) {
	return fmt.Sprintf("MUTATED_%s", rVal), false, nil
}

type CustomMutator struct{}

func (m *CustomMutator) EnvMutate(ctx context.Context, oKey, rKey, oVal, rVal string) (string, bool, error) {
	return fmt.Sprintf("CUSTOM_MUTATED_%s", rVal), false, nil
}

// Electron > Lepton > Quark
type Electron struct {
	Name   string `env:"ELECTRON_NAME"`
	Lepton *Lepton
}

type Lepton struct {
	Name  string `env:"LEPTON_NAME"`
	Quark *Quark
}

type Quark struct {
	Value int8 `env:"QUARK_VALUE"`
}

// Sandwich > Bread > Meat
type Sandwich struct {
	Name  string `env:"SANDWICH_NAME"`
	Bread Bread
}

type Bread struct {
	Name string `env:"BREAD_NAME"`
	Meat Meat
}

type Meat struct {
	Type string `env:"MEAT_TYPE"`
}

// Prefixes
type TV struct {
	Remote *Remote `env:",prefix=TV_"`
	Name   string  `env:"NAME"`
}

type VCR struct {
	Remote Remote `env:",prefix=VCR_"`
	Name   string `env:"NAME"`
}

type Remote struct {
	Button *Button `env:",prefix=REMOTE_BUTTON_"`
	Name   string  `env:"REMOTE_NAME,required"`
	Power  bool    `env:"POWER"`
}

type Button struct {
	Name string `env:"NAME, default=POWER"`
}

type SliceStruct struct {
	Field []string `env:"FIELD"`
}

type MapStruct struct {
	Field map[string]string `env:"FIELD"`
}

type Base64ByteSlice []Base64Bytes

func TestProcessWithConfigArgument(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	lookuper := MapLookuper(map[string]string{
		"MEAT_TYPE": "chicken",
	})

	firstMutator := MutatorFunc(func(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (newValue string, stop bool, err error) {
		if originalKey == "MEAT_TYPE" && currentValue == "chicken" {
			return "pork", false, nil
		}
		return currentValue, false, nil
	})

	secondMutator := MutatorFunc(func(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (newValue string, stop bool, err error) {
		if originalKey == "MEAT_TYPE" && currentValue == "pork" {
			return "fish", false, nil
		}
		return currentValue, false, nil
	})

	var meatConfig Meat
	cfg := &Config{
		Target:   &meatConfig,
		Lookuper: lookuper,
		Mutators: []Mutator{firstMutator},
	}

	err := Process(ctx, cfg, secondMutator)
	if err != nil {
		t.Errorf("unexpected error from Process: %s", err.Error())
	}

	if diff := cmp.Diff(meatConfig.Type, "fish"); diff != "" {
		t.Errorf("wrong value in meatConfig. Diff (-got +want): %s", diff)
	}
}

func TestProcessWith(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		target         any
		lookuper       Lookuper
		defDelimiter   string
		defSeparator   string
		defNoInit      bool
		defOverwrite   bool
		defDecodeUnset bool
		defRequired    bool
		mutators       []Mutator
		exp            any
		err            error
		errMsg         string
	}{
		// nil pointer
		{
			name:     "nil",
			target:   (*Electron)(nil),
			lookuper: MapLookuper(nil),
			err:      ErrNotStruct,
		},

		// Bool
		{
			name: "bool/true",
			target: &struct {
				Field bool `env:"FIELD"`
			}{},
			exp: &struct {
				Field bool `env:"FIELD"`
			}{
				Field: true,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "true",
			}),
		},
		{
			name: "bool/false",
			target: &struct {
				Field bool `env:"FIELD"`
			}{},
			exp: &struct {
				Field bool `env:"FIELD"`
			}{
				Field: false,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "false",
			}),
		},
		{
			name: "bool/error",
			target: &struct {
				Field bool `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid bool",
			}),
			errMsg: "invalid syntax",
		},

		// Float
		{
			name: "float32/6.022",
			target: &struct {
				Field float32 `env:"FIELD"`
			}{},
			exp: &struct {
				Field float32 `env:"FIELD"`
			}{
				Field: 6.022,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "6.022",
			}),
		},
		{
			name: "float32/error",
			target: &struct {
				Field float32 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid float",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "float64/6.022",
			target: &struct {
				Field float64 `env:"FIELD"`
			}{},
			exp: &struct {
				Field float64 `env:"FIELD"`
			}{
				Field: 6.022,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "6.022",
			}),
		},
		{
			name: "float32/error",
			target: &struct {
				Field float64 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid float",
			}),
			errMsg: "invalid syntax",
		},

		// Int8-32
		{
			name: "int/8675309",
			target: &struct {
				Field int `env:"FIELD"`
			}{},
			exp: &struct {
				Field int `env:"FIELD"`
			}{
				Field: 8675309,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "8675309",
			}),
		},
		{
			name: "int/error",
			target: &struct {
				Field int `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "int8/12",
			target: &struct {
				Field int8 `env:"FIELD"`
			}{},
			exp: &struct {
				Field int8 `env:"FIELD"`
			}{
				Field: 12,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "12",
			}),
		},
		{
			name: "int8/error",
			target: &struct {
				Field int8 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "int16/1245",
			target: &struct {
				Field int16 `env:"FIELD"`
			}{},
			exp: &struct {
				Field int16 `env:"FIELD"`
			}{
				Field: 12345,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "12345",
			}),
		},
		{
			name: "int16/error",
			target: &struct {
				Field int16 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "int32/1245",
			target: &struct {
				Field int32 `env:"FIELD"`
			}{},
			exp: &struct {
				Field int32 `env:"FIELD"`
			}{
				Field: 12345,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "12345",
			}),
		},
		{
			name: "int32/error",
			target: &struct {
				Field int32 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},

		// Int64
		{
			name: "int64/1245",
			target: &struct {
				Field int64 `env:"FIELD"`
			}{},
			exp: &struct {
				Field int64 `env:"FIELD"`
			}{
				Field: 12345,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "12345",
			}),
		},
		{
			name: "int64/error",
			target: &struct {
				Field int64 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "int64/duration",
			target: &struct {
				Field time.Duration `env:"FIELD"`
			}{},
			exp: &struct {
				Field time.Duration `env:"FIELD"`
			}{
				Field: 10 * time.Second,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "10s",
			}),
		},
		{
			name: "int64/duration_pointer",
			target: &struct {
				Field *time.Duration `env:"FIELD"`
			}{},
			exp: &struct {
				Field *time.Duration `env:"FIELD"`
			}{
				Field: ptrTo(10 * time.Second),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "10s",
			}),
		},
		{
			name: "int64/duration_error",
			target: &struct {
				Field time.Duration `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid time",
			}),
			errMsg: "invalid duration",
		},

		// String
		{
			name: "string",
			target: &struct {
				Field string `env:"FIELD"`
			}{},
			exp: &struct {
				Field string `env:"FIELD"`
			}{
				Field: "foo",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},

		// Uint8-64
		{
			name: "uint/8675309",
			target: &struct {
				Field uint `env:"FIELD"`
			}{},
			exp: &struct {
				Field uint `env:"FIELD"`
			}{
				Field: 8675309,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "8675309",
			}),
		},
		{
			name: "uint/error",
			target: &struct {
				Field uint `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid uint",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "uint8/12",
			target: &struct {
				Field uint8 `env:"FIELD"`
			}{},
			exp: &struct {
				Field uint8 `env:"FIELD"`
			}{
				Field: 12,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "12",
			}),
		},
		{
			name: "uint8/error",
			target: &struct {
				Field uint8 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid uint",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "uint16/1245",
			target: &struct {
				Field uint16 `env:"FIELD"`
			}{},
			exp: &struct {
				Field uint16 `env:"FIELD"`
			}{
				Field: 12345,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "12345",
			}),
		},
		{
			name: "uint16/error",
			target: &struct {
				Field uint16 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid uint",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "uint32/1245",
			target: &struct {
				Field uint32 `env:"FIELD"`
			}{},
			exp: &struct {
				Field uint32 `env:"FIELD"`
			}{
				Field: 12345,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "12345",
			}),
		},
		{
			name: "uint32/error",
			target: &struct {
				Field uint32 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "uint64/1245",
			target: &struct {
				Field uint64 `env:"FIELD"`
			}{},
			exp: &struct {
				Field uint64 `env:"FIELD"`
			}{
				Field: 12345,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "12345",
			}),
		},
		{
			name: "uint64/error",
			target: &struct {
				Field uint64 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "uintptr/1245",
			target: &struct {
				Field uintptr `env:"FIELD"`
			}{},
			exp: &struct {
				Field uintptr `env:"FIELD"`
			}{
				Field: 12345,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "12345",
			}),
		},
		{
			name: "uintptr/error",
			target: &struct {
				Field uintptr `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},

		// Map
		{
			name: "map/single",
			target: &struct {
				Field map[string]string `env:"FIELD"`
			}{},
			exp: &struct {
				Field map[string]string `env:"FIELD"`
			}{
				Field: map[string]string{"foo": "bar"},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo:bar",
			}),
		},
		{
			name: "map/multi",
			target: &struct {
				Field map[string]string `env:"FIELD"`
			}{},
			exp: &struct {
				Field map[string]string `env:"FIELD"`
			}{
				Field: map[string]string{
					"foo":  "bar",
					"zip":  "zap",
					"zing": "zang",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo:bar,zip:zap,zing:zang",
			}),
		},
		{
			name: "map/empty",
			target: &struct {
				Field map[string]string `env:"FIELD"`
			}{},
			exp: &struct {
				Field map[string]string `env:"FIELD"`
			}{
				Field: nil,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "",
			}),
		},
		{
			name: "map/key_no_value",
			target: &struct {
				Field map[string]string `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
			errMsg: "invalid map item",
		},
		{
			name: "map/key_error",
			target: &struct {
				Field map[bool]bool `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "nope:true",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "map/value_error",
			target: &struct {
				Field map[bool]bool `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "true:nope",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "map/custom_delimiter",
			target: &struct {
				Field map[string]string `env:"FIELD,delimiter=;"`
			}{},
			exp: &struct {
				Field map[string]string `env:"FIELD,delimiter=;"`
			}{
				Field: map[string]string{"foo": "1,2", "bar": "3,4", "zip": "zap:zoo,3"},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo:1,2;bar:3,4; zip:zap:zoo,3",
			}),
		},
		{
			name: "map/custom_separator",
			target: &struct {
				Field map[string]string `env:"FIELD,separator=="`
			}{},
			exp: &struct {
				Field map[string]string `env:"FIELD,separator=="`
			}{
				Field: map[string]string{"foo": "bar", "zip:zap": "zoo:zil"},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo=bar, zip:zap=zoo:zil",
			}),
		},
		{
			name: "map/custom_separator_error",
			target: &struct {
				Field map[string]string `env:"FIELD,separator=="`
			}{},
			exp: &struct {
				Field map[string]string `env:"FIELD,separator=="`
			}{
				Field: map[string]string{"foo": "bar"},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo:bar",
			}),
			errMsg: "invalid map item",
		},

		// Slices
		{
			name: "slice/single",
			target: &struct {
				Field []string `env:"FIELD"`
			}{},
			exp: &struct {
				Field []string `env:"FIELD"`
			}{
				Field: []string{"foo"},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "slice/multi",
			target: &struct {
				Field []string `env:"FIELD"`
			}{},
			exp: &struct {
				Field []string `env:"FIELD"`
			}{
				Field: []string{"foo", "bar"},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo,bar",
			}),
		},
		{
			name: "slice/empty",
			target: &struct {
				Field []string `env:"FIELD"`
			}{},
			exp: &struct {
				Field []string `env:"FIELD"`
			}{
				Field: nil,
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "",
			}),
		},
		{
			name: "slice/bytes",
			target: &struct {
				Field []byte `env:"FIELD"`
			}{},
			exp: &struct {
				Field []byte `env:"FIELD"`
			}{
				Field: []byte("foo"),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "slice/custom_delimiter",
			target: &struct {
				Field []string `env:"FIELD,delimiter=;"`
			}{},
			exp: &struct {
				Field []string `env:"FIELD,delimiter=;"`
			}{
				Field: []string{"foo,bar", "baz"},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo,bar;baz",
			}),
		},

		// Private fields
		{
			name: "private/noop",
			target: &struct {
				field string
			}{},
			exp: &struct {
				field string
			}{
				field: "",
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "private/error",
			target: &struct {
				field string `env:"FIELD"`
			}{},
			exp: &struct {
				field string `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
			err: ErrPrivateField,
		},

		// Overwrite
		{
			name: "overwrite/present",
			target: &struct {
				Field string `env:"FIELD,overwrite"`
			}{
				Field: "hello world",
			},
			exp: &struct {
				Field string `env:"FIELD,overwrite"`
			}{
				Field: "foo",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "overwrite/present_space",
			target: &struct {
				Field string `env:"FIELD, overwrite"`
			}{
				Field: "hello world",
			},
			exp: &struct {
				Field string `env:"FIELD, overwrite"`
			}{
				Field: "foo",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "overwrite/does_not_overwrite_no_value",
			target: &struct {
				Field string `env:"FIELD, overwrite"`
			}{
				Field: "inside",
			},
			exp: &struct {
				Field string `env:"FIELD, overwrite"`
			}{
				Field: "inside",
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "overwrite/env_overwrites_existing",
			target: &struct {
				Field string `env:"FIELD, overwrite"`
			}{
				Field: "inside",
			},
			exp: &struct {
				Field string `env:"FIELD, overwrite"`
			}{
				Field: "env",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "env",
			}),
		},
		{
			name: "overwrite/env_overwrites_empty",
			target: &struct {
				Field string `env:"FIELD, overwrite"`
			}{},
			exp: &struct {
				Field string `env:"FIELD, overwrite"`
			}{
				Field: "env",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "env",
			}),
		},
		{
			name: "overwrite/default_does_not_overwrite_no_value",
			target: &struct {
				Field string `env:"FIELD, overwrite, default=default"`
			}{
				Field: "inside",
			},
			exp: &struct {
				Field string `env:"FIELD, overwrite, default=default"`
			}{
				Field: "inside",
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "overwrite/default_env_overwrites_existing",
			target: &struct {
				Field string `env:"FIELD, overwrite, default=default"`
			}{
				Field: "inside",
			},
			exp: &struct {
				Field string `env:"FIELD, overwrite, default=default"`
			}{
				Field: "env",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "env",
			}),
		},
		{
			name: "overwrite/default_env_overwrites_empty",
			target: &struct {
				Field string `env:"FIELD, overwrite, default=default"`
			}{},
			exp: &struct {
				Field string `env:"FIELD, overwrite, default=default"`
			}{
				Field: "env",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "env",
			}),
		},
		{
			name: "overwrite/default_uses_default_when_unspecified",
			target: &struct {
				Field string `env:"FIELD, overwrite, default=default"`
			}{},
			exp: &struct {
				Field string `env:"FIELD, overwrite, default=default"`
			}{
				Field: "default",
			},
			lookuper: MapLookuper(nil),
		},

		// Decode Unset
		{
			name: "decodeunset/present",
			target: &struct {
				Field Level `env:"FIELD,decodeunset"`
			}{},
			exp: &struct {
				Field Level `env:"FIELD,decodeunset"`
			}{
				Field: LevelInfo,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "decodeunset/present_space",
			target: &struct {
				Field Level `env:"FIELD, decodeunset"`
			}{},
			exp: &struct {
				Field Level `env:"FIELD, decodeunset"`
			}{
				Field: LevelInfo,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "decodeunset/present_camelcase",
			target: &struct {
				Field Level `env:"FIELD, decodeUnset"`
			}{},
			exp: &struct {
				Field Level `env:"FIELD, decodeUnset"`
			}{
				Field: LevelInfo,
			},
			lookuper: MapLookuper(nil),
		},

		// Required
		{
			name: "required/present",
			target: &struct {
				Field string `env:"FIELD,required"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,required"`
			}{
				Field: "foo",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "required/present_space",
			target: &struct {
				Field string `env:"FIELD, required"`
			}{},
			exp: &struct {
				Field string `env:"FIELD, required"`
			}{
				Field: "foo",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "required/missing",
			target: &struct {
				Field string `env:"FIELD,required"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrMissingRequired,
		},
		{
			name: "required/missing_space",
			target: &struct {
				Field string `env:"FIELD, required"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrMissingRequired,
		},
		{
			name: "required/default",
			target: &struct {
				Field string `env:"FIELD,required,default=foo"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrRequiredAndDefault,
		},
		{
			name: "required/default_space",
			target: &struct {
				Field string `env:"FIELD, required, default=foo"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrRequiredAndDefault,
		},

		// Default
		{
			name: "default/missing",
			target: &struct {
				Field string `env:"FIELD,default=foo"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,default=foo"`
			}{
				Field: "foo", // uses default
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "default/missing_space",
			target: &struct {
				Field string `env:"FIELD, default=foo"`
			}{},
			exp: &struct {
				Field string `env:"FIELD, default=foo"`
			}{
				Field: "foo", // uses default
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "default/empty",
			target: &struct {
				Field string `env:"FIELD,default=foo"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,default=foo"`
			}{
				Field: "", // doesn't use default
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "",
			}),
		},
		{
			name: "default/empty_space",
			target: &struct {
				Field string `env:"FIELD, default=foo"`
			}{},
			exp: &struct {
				Field string `env:"FIELD, default=foo"`
			}{
				Field: "", // doesn't use default
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "",
			}),
		},
		{
			name: "default/spaces",
			target: &struct {
				Field string `env:"FIELD, default=foo bar baz"`
			}{},
			exp: &struct {
				Field string `env:"FIELD, default=foo bar baz"`
			}{
				Field: "", // doesn't use default
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "",
			}),
		},
		{
			name: "default/spaces_expand",
			target: &struct {
				Field1 string `env:"FIELD1, default=foo"`
				Field2 string `env:"FIELD2, default=bar $FIELD1"`
			}{},
			exp: &struct {
				Field1 string `env:"FIELD1, default=foo"`
				Field2 string `env:"FIELD2, default=bar $FIELD1"`
			}{
				Field1: "one",
				Field2: "bar one",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD1": "one",
			}),
		},
		{
			name: "default/expand",
			target: &struct {
				Field string `env:"FIELD,default=$DEFAULT"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,default=$DEFAULT"`
			}{
				Field: "bar",
			},
			lookuper: MapLookuper(map[string]string{
				"DEFAULT": "bar",
			}),
		},
		{
			name: "default/expand_space",
			target: &struct {
				Field string `env:"FIELD, default=$DEFAULT"`
			}{},
			exp: &struct {
				Field string `env:"FIELD, default=$DEFAULT"`
			}{
				Field: "bar",
			},
			lookuper: MapLookuper(map[string]string{
				"DEFAULT": "bar",
			}),
		},
		{
			name: "default/expand_empty",
			target: &struct {
				Field string `env:"FIELD,default=$DEFAULT"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,default=$DEFAULT"`
			}{
				Field: "",
			},
			lookuper: MapLookuper(map[string]string{
				"DEFAULT": "",
			}),
		},
		{
			name: "default/expand_nil",
			target: &struct {
				Field string `env:"FIELD,default=$DEFAULT"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,default=$DEFAULT"`
			}{
				Field: "",
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "default/expand_nil_typed",
			target: &struct {
				Field bool `env:"FIELD,default=$DEFAULT"`
			}{},
			exp: &struct {
				Field bool `env:"FIELD,default=$DEFAULT"`
			}{
				Field: false,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "default/expand_prefix",
			target: &struct {
				Field string `env:"FIELD,default=$DEFAULT"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,default=$DEFAULT"`
			}{
				Field: "value",
			},
			lookuper: PrefixLookuper("PREFIX_", MapLookuper(map[string]string{
				// Ensure that we use the underlying MapLookuper instead of the prefixed
				// value when resolving a default:
				//
				//     https://github.com/sethvargo/go-envconfig/issues/85
				//
				"DEFAULT": "value",
			})),
		},
		{
			name: "default/expand_prefix_prefix",
			target: &struct {
				Field string `env:"FIELD,default=$DEFAULT"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,default=$DEFAULT"`
			}{
				Field: "value",
			},
			lookuper: PrefixLookuper("OUTER_", PrefixLookuper("INNER_", MapLookuper(map[string]string{
				// Ensure that we use the underlying MapLookuper instead of the prefixed
				// value when resolving a default:
				//
				//     https://github.com/sethvargo/go-envconfig/issues/85
				//
				"DEFAULT": "value",
			}))),
		},
		{
			name: "default/escaped_doesnt_interpolate",
			target: &struct {
				Field string `env:"FIELD,default=\\$DEFAULT"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,default=\\$DEFAULT"`
			}{
				Field: "$DEFAULT",
			},
			lookuper: MapLookuper(map[string]string{
				"DEFAULT": "should-not-be-replaced",
			}),
		},
		{
			name: "default/escaped_escaped_keeps_escape",
			target: &struct {
				Field string `env:"FIELD,default=C:\\Personal\\\\$DEFAULT"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,default=C:\\Personal\\\\$DEFAULT"`
			}{
				Field: `C:\Personal\value`,
			},
			lookuper: MapLookuper(map[string]string{
				"DEFAULT": "value",
			}),
		},
		{
			name: "default/slice",
			target: &struct {
				Field []string `env:"FIELD,default=foo,bar,baz"`
			}{},
			exp: &struct {
				Field []string `env:"FIELD,default=foo,bar,baz"`
			}{
				Field: []string{"foo", "bar", "baz"},
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "default/slice_space",
			target: &struct {
				Field []string `env:"FIELD, default=foo,bar,baz"`
			}{},
			exp: &struct {
				Field []string `env:"FIELD, default=foo,bar,baz"`
			}{
				Field: []string{"foo", "bar", "baz"},
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "default/map",
			target: &struct {
				Field map[string]string `env:"FIELD,default=foo:bar,zip:zap"`
			}{},
			exp: &struct {
				Field map[string]string `env:"FIELD,default=foo:bar,zip:zap"`
			}{
				Field: map[string]string{"foo": "bar", "zip": "zap"},
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "default/map_spaces",
			target: &struct {
				Field map[string]string `env:"FIELD, default=foo:bar,zip:zap"`
			}{},
			exp: &struct {
				Field map[string]string `env:"FIELD, default=foo:bar,zip:zap"`
			}{
				Field: map[string]string{"foo": "bar", "zip": "zap"},
			},
			lookuper: MapLookuper(nil),
		},

		// Syntax
		{
			name: "syntax/=key",
			target: &struct {
				Field CustomDecoderType `env:"FIELD=foo"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrInvalidEnvvarName,
		},

		// Custom decoding from standard library interfaces
		{
			name: "custom_decoder/gob_decoder",
			target: &struct {
				Field CustomStdLibDecodingType `env:"FIELD"`
			}{
				Field: CustomStdLibDecodingType{
					implementsGobDecoder: true,
				},
			},
			exp: &struct {
				Field CustomStdLibDecodingType `env:"FIELD"`
			}{
				Field: CustomStdLibDecodingType{
					value: "GOB-foo",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "custom_decoder/binary_unmarshaler",
			target: &struct {
				Field CustomStdLibDecodingType `env:"FIELD"`
			}{
				Field: CustomStdLibDecodingType{
					implementsBinaryUnmarshaler: true,
					implementsGobDecoder:        true,
				},
			},
			exp: &struct {
				Field CustomStdLibDecodingType `env:"FIELD"`
			}{
				Field: CustomStdLibDecodingType{
					value: "BINARY-foo",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "custom_decoder/json_unmarshaler",
			target: &struct {
				Field CustomStdLibDecodingType `env:"FIELD"`
			}{
				Field: CustomStdLibDecodingType{
					implementsBinaryUnmarshaler: true,
					implementsJSONUnmarshaler:   true,
					implementsGobDecoder:        true,
				},
			},
			exp: &struct {
				Field CustomStdLibDecodingType `env:"FIELD"`
			}{
				Field: CustomStdLibDecodingType{
					implementsTextUnmarshaler: true,
					value:                     "JSON-foo",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "custom_decoder/text_unmarshaler",
			target: &struct {
				Field CustomStdLibDecodingType `env:"FIELD"`
			}{
				Field: CustomStdLibDecodingType{
					implementsTextUnmarshaler:   true,
					implementsBinaryUnmarshaler: true,
					implementsJSONUnmarshaler:   true,
					implementsGobDecoder:        true,
				},
			},
			exp: &struct {
				Field CustomStdLibDecodingType `env:"FIELD"`
			}{
				Field: CustomStdLibDecodingType{
					implementsTextUnmarshaler: true,
					value:                     "TEXT-foo",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},

		// Custom decoder
		{
			name: "custom_decoder/struct",
			target: &struct {
				Field CustomDecoderType `env:"FIELD"`
			}{},
			exp: &struct {
				Field CustomDecoderType `env:"FIELD"`
			}{
				Field: CustomDecoderType{
					value: "CUSTOM-foo",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "custom_decoder/pointer",
			target: &struct {
				Field *CustomDecoderType `env:"FIELD"`
			}{},
			exp: &struct {
				Field *CustomDecoderType `env:"FIELD"`
			}{
				Field: &CustomDecoderType{
					value: "CUSTOM-foo",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "custom_decoder/private",
			target: &struct {
				field *CustomDecoderType `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
			err: ErrPrivateField,
		},
		{
			name: "custom_decoder/error",
			target: &struct {
				Field CustomTypeError `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
			errMsg: "broken",
		},
		{
			name: "custom_decoder/called_for_empty_string",
			target: &struct {
				Field CustomTypeError `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "",
			}),
			errMsg: "broken",
		},
		{
			name: "custom_decoder/called_when_default",
			target: &struct {
				Field *CustomDecoderType `env:"FIELD, default=foo"`
			}{},
			exp: &struct {
				Field *CustomDecoderType `env:"FIELD, default=foo"`
			}{
				Field: &CustomDecoderType{
					value: "CUSTOM-foo",
				},
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "custom_decoder/called_on_decodeunset",
			target: &struct {
				Field *CustomDecoderType `env:"FIELD, decodeunset"`
			}{},
			exp: &struct {
				Field *CustomDecoderType `env:"FIELD, decodeunset"`
			}{
				Field: &CustomDecoderType{
					value: "CUSTOM-",
				},
			},
			lookuper: MapLookuper(nil),
		},

		// Expand
		{
			name: "expand/not_default",
			target: &struct {
				Field string `env:"FIELD"`
			}{},
			exp: &struct {
				Field string `env:"FIELD"`
			}{
				Field: "$VALUE",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "$VALUE",
			}),
		},

		// Pointer pointers
		{
			name: "pointer_string",
			target: &struct {
				Field *string `env:"FIELD"`
			}{},
			exp: &struct {
				Field *string `env:"FIELD"`
			}{
				Field: ptrTo("foo"),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "pointer_pointer_string",
			target: &struct {
				Field **string `env:"FIELD"`
			}{},
			exp: &struct {
				Field **string `env:"FIELD"`
			}{
				Field: ptrTo(ptrTo("foo")),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "pointer_int",
			target: &struct {
				Field *int `env:"FIELD"`
			}{},
			exp: &struct {
				Field *int `env:"FIELD"`
			}{
				Field: ptrTo(int(5)),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "5",
			}),
		},
		{
			name: "pointer_map",
			target: &struct {
				Field *map[string]string `env:"FIELD"`
			}{},
			exp: &struct {
				Field *map[string]string `env:"FIELD"`
			}{
				Field: ptrTo(map[string]string{"foo": "bar"}),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo:bar",
			}),
		},
		{
			name: "pointer_slice",
			target: &struct {
				Field *[]string `env:"FIELD"`
			}{},
			exp: &struct {
				Field *[]string `env:"FIELD"`
			}{
				Field: ptrTo([]string{"foo"}),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "pointer_bool",
			target: &struct {
				Field *bool `env:"FIELD"`
			}{},
			exp: &struct {
				Field *bool `env:"FIELD"`
			}{
				Field: ptrTo(true),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "true",
			}),
		},
		{
			name: "pointer_bool_noinit",
			target: &struct {
				Field *bool `env:"FIELD,noinit"`
			}{},
			exp: &struct {
				Field *bool `env:"FIELD,noinit"`
			}{
				Field: nil,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "pointer_bool_default_field_set_env_unset",
			target: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: ptrTo(false),
			},
			exp: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: ptrTo(false),
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "pointer_bool_default_field_set_env_set",
			target: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: ptrTo(false),
			},
			exp: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: ptrTo(false),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "true",
			}),
		},
		{
			name: "pointer_bool_default_field_unset_env_set",
			target: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{},
			exp: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: ptrTo(false),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "false",
			}),
		},
		{
			name: "pointer_bool_default_field_unset_env_unset",
			target: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{},
			exp: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: ptrTo(true),
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "pointer_bool_default_overwrite_field_set_env_unset",
			target: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: ptrTo(false),
			},
			exp: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: ptrTo(false),
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "pointer_bool_default_overwrite_field_set_env_set",
			target: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: ptrTo(false),
			},
			exp: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: ptrTo(true),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "true",
			}),
		},
		{
			name: "pointer_bool_default_overwrite_field_unset_env_set",
			target: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{},
			exp: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: ptrTo(false),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "false",
			}),
		},
		{
			name: "pointer_bool_default_overwrite_field_unset_env_unset",
			target: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{},
			exp: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: ptrTo(true),
			},
			lookuper: MapLookuper(nil),
		},

		// Marshallers
		{
			name: "binarymarshaler",
			target: &struct {
				Field url.URL `env:"FIELD"`
			}{},
			exp: &struct {
				Field url.URL `env:"FIELD"`
			}{
				Field: url.URL{
					Scheme: "http",
					Host:   "simple.test",
					Path:   "/",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "http://simple.test/",
			}),
		},
		{
			name: "binarymarshaler_pointer",
			target: &struct {
				Field *url.URL `env:"FIELD"`
			}{},
			exp: &struct {
				Field *url.URL `env:"FIELD"`
			}{
				Field: &url.URL{
					Scheme: "http",
					Host:   "simple.test",
					Path:   "/",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "http://simple.test/",
			}),
		},
		{
			name: "gob",
			target: &struct {
				Field time.Time `env:"FIELD"`
			}{},
			exp: &struct {
				Field time.Time `env:"FIELD"`
			}{
				Field: time.Unix(0, 0),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": func() string {
					v, _ := time.Unix(0, 0).GobEncode()
					return string(v)
				}(),
			}),
		},
		{
			name: "gob_pointer",
			target: &struct {
				Field *time.Time `env:"FIELD"`
			}{},
			exp: &struct {
				Field *time.Time `env:"FIELD"`
			}{
				Field: ptrTo(time.Unix(0, 0)),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": func() string {
					v, _ := time.Unix(0, 0).GobEncode()
					return string(v)
				}(),
			}),
		},
		{
			name: "jsonmarshaler",
			target: &struct {
				Field time.Time `env:"FIELD"`
			}{},
			exp: &struct {
				Field time.Time `env:"FIELD"`
			}{
				Field: time.Unix(0, 0),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": func() string {
					v, _ := time.Unix(0, 0).MarshalJSON()
					return string(v)
				}(),
			}),
		},
		{
			name: "jsonmarshaler_pointer",
			target: &struct {
				Field *time.Time `env:"FIELD"`
			}{},
			exp: &struct {
				Field *time.Time `env:"FIELD"`
			}{
				Field: ptrTo(time.Unix(0, 0)),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": func() string {
					v, _ := time.Unix(0, 0).MarshalJSON()
					return string(v)
				}(),
			}),
		},
		{
			name: "textmarshaler",
			target: &struct {
				Field time.Time `env:"FIELD"`
			}{},
			exp: &struct {
				Field time.Time `env:"FIELD"`
			}{
				Field: time.Unix(0, 0),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": func() string {
					v, _ := time.Unix(0, 0).MarshalText()
					return string(v)
				}(),
			}),
		},
		{
			name: "textmarshaler_pointer",
			target: &struct {
				Field *time.Time `env:"FIELD"`
			}{},
			exp: &struct {
				Field *time.Time `env:"FIELD"`
			}{
				Field: ptrTo(time.Unix(0, 0)),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": func() string {
					v, _ := time.Unix(0, 0).MarshalText()
					return string(v)
				}(),
			}),
		},

		// Mutators
		{
			name: "mutate",
			target: &struct {
				Field string `env:"FIELD"`
			}{},
			exp: &struct {
				Field string `env:"FIELD"`
			}{
				Field: "MUTATED_value",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "value",
			}),
			mutators: []Mutator{valueMutatorFunc},
		},
		{
			name: "mutate/custom",
			target: &struct {
				Field string `env:"FIELD"`
			}{},
			exp: &struct {
				Field string `env:"FIELD"`
			}{
				Field: "CUSTOM_MUTATED_value",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "value",
			}),
			mutators: []Mutator{
				&CustomMutator{},
			},
		},
		{
			name: "mutate/stops",
			target: &struct {
				Field string `env:"FIELD"`
			}{},
			exp: &struct {
				Field string `env:"FIELD"`
			}{
				Field: "value-1",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "",
			}),
			mutators: []Mutator{
				MutatorFunc(func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return "value-1", true, nil
				}),
				MutatorFunc(func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return "value-2", true, nil
				}),
			},
		},
		{
			name: "mutate/original_and_resolved_keys",
			target: &struct {
				Field string `env:"FIELD"`
			}{},
			exp: &struct {
				Field string `env:"FIELD"`
			}{
				Field: "oKey:FIELD, rKey:KEY_FIELD",
			},
			lookuper: PrefixLookuper("KEY_", MapLookuper(map[string]string{
				"KEY_FIELD": "",
			})),
			mutators: []Mutator{
				MutatorFunc(func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return fmt.Sprintf("oKey:%s, rKey:%s", oKey, rKey), false, nil
				}),
			},
		},
		{
			name: "mutate/original_and_current_values",
			target: &struct {
				Field string `env:"FIELD"`
			}{},
			exp: &struct {
				Field string `env:"FIELD"`
			}{
				Field: "oVal:old-value, cVal:new-value",
			},
			lookuper: PrefixLookuper("KEY_", MapLookuper(map[string]string{
				"KEY_FIELD": "old-value",
			})),
			mutators: []Mutator{
				MutatorFunc(func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return "new-value", false, nil
				}),
				MutatorFunc(func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return fmt.Sprintf("oVal:%s, cVal:%s", oVal, cVal), false, nil
				}),
			},
		},
		{
			name: "mutate/halts_error",
			target: &struct {
				Field string `env:"FIELD"`
			}{},
			exp: &struct {
				Field string `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "",
			}),
			mutators: []Mutator{
				MutatorFunc(func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return "", false, fmt.Errorf("error 1")
				}),
				MutatorFunc(func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return "", false, fmt.Errorf("error 2")
				}),
			},
			errMsg: "error 1",
		},

		// Nesting
		{
			name:   "nested_pointer_structs",
			target: &Electron{},
			exp: &Electron{
				Name: "shocking",
				Lepton: &Lepton{
					Name: "tea?",
					Quark: &Quark{
						Value: 2,
					},
				},
			},
			lookuper: MapLookuper(map[string]string{
				"ELECTRON_NAME": "shocking",
				"LEPTON_NAME":   "tea?",
				"QUARK_VALUE":   "2",
			}),
		},
		{
			name:   "nested_structs",
			target: &Sandwich{},
			exp: &Sandwich{
				Name: "yummy",
				Bread: Bread{
					Name: "rye",
					Meat: Meat{
						Type: "pep",
					},
				},
			},
			lookuper: MapLookuper(map[string]string{
				"SANDWICH_NAME": "yummy",
				"BREAD_NAME":    "rye",
				"MEAT_TYPE":     "pep",
			}),
		},
		{
			name:   "nested_mutation",
			target: &Sandwich{},
			exp: &Sandwich{
				Name: "MUTATED_yummy",
				Bread: Bread{
					Name: "MUTATED_rye",
					Meat: Meat{
						Type: "MUTATED_pep",
					},
				},
			},
			lookuper: MapLookuper(map[string]string{
				"SANDWICH_NAME": "yummy",
				"BREAD_NAME":    "rye",
				"MEAT_TYPE":     "pep",
			}),
			mutators: []Mutator{valueMutatorFunc},
		},

		// Overwriting
		{
			name: "no_overwrite/structs",
			target: &Electron{
				Name: "original",
				Lepton: &Lepton{
					Name: "original",
					Quark: &Quark{
						Value: 1,
					},
				},
			},
			exp: &Electron{
				Name: "original",
				Lepton: &Lepton{
					Name: "original",
					Quark: &Quark{
						Value: 1,
					},
				},
			},
			lookuper: MapLookuper(map[string]string{
				"ELECTRON_NAME": "shocking",
				"LEPTON_NAME":   "tea?",
				"QUARK_VALUE":   "2",
			}),
		},
		{
			name: "no_overwrite/pointers",
			target: &struct {
				Field *string `env:"FIELD"`
			}{
				Field: ptrTo("bar"),
			},
			exp: &struct {
				Field *string `env:"FIELD"`
			}{
				Field: ptrTo("bar"),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "no_overwrite/pointers_pointers",
			target: &struct {
				Field **string `env:"FIELD"`
			}{
				Field: ptrTo(ptrTo("bar")),
			},
			exp: &struct {
				Field **string `env:"FIELD"`
			}{
				Field: ptrTo(ptrTo("bar")),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},

		// Unknown options
		{
			name: "unknown_options",
			target: &struct {
				Field string `env:"FIELD,cookies"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrUnknownOption,
		},

		// Lookup prefixes
		{
			name: "lookup_prefixes",
			target: &struct {
				Field string `env:"FIELD"`
			}{},
			exp: &struct {
				Field string `env:"FIELD"`
			}{
				Field: "bar",
			},
			lookuper: PrefixLookuper("FOO_", MapLookuper(map[string]string{
				"FOO_FIELD": "bar",
			})),
		},

		// Embedded prefixes
		{
			name:   "embedded_prefixes/pointers",
			target: &TV{},
			exp: &TV{
				Name: "tv",
				Remote: &Remote{
					Button: &Button{
						Name: "button",
					},
					Name: "remote",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"NAME":                  "tv",
				"TV_REMOTE_NAME":        "remote",
				"TV_REMOTE_BUTTON_NAME": "button",
			}),
		},
		{
			name:   "embedded_prefixes/values",
			target: &VCR{},
			exp: &VCR{
				Name: "vcr",
				Remote: Remote{
					Button: &Button{
						Name: "button",
					},
					Name: "remote",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"NAME":                   "vcr",
				"VCR_REMOTE_NAME":        "remote",
				"VCR_REMOTE_BUTTON_NAME": "button",
			}),
		},
		{
			name:   "embedded_prefixes/defaults",
			target: &TV{},
			exp: &TV{
				Name: "tv",
				Remote: &Remote{
					Button: &Button{
						Name: "POWER",
					},
					Name: "remote",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"NAME":           "tv",
				"TV_REMOTE_NAME": "remote",
			}),
		},
		{
			name: "embedded_prefixes/error",
			target: &struct {
				Field string `env:",prefix=FIELD_"`
			}{},
			err:      ErrPrefixNotStruct,
			lookuper: MapLookuper(nil),
		},

		// Init (default behavior)
		{
			name: "init/basic",
			target: &struct {
				Field1 string    `env:"FIELD1"`
				Field2 bool      `env:"FIELD2"`
				Field3 int64     `env:"FIELD3"`
				Field4 float64   `env:"FIELD4"`
				Field5 complex64 `env:"FIELD5"`
			}{},
			exp: &struct {
				Field1 string    `env:"FIELD1"`
				Field2 bool      `env:"FIELD2"`
				Field3 int64     `env:"FIELD3"`
				Field4 float64   `env:"FIELD4"`
				Field5 complex64 `env:"FIELD5"`
			}{
				Field1: "",
				Field2: false,
				Field3: 0,
				Field4: 0,
				Field5: 0,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "init/structs",
			target: &struct {
				Electron *Electron
			}{},
			exp: &struct {
				Electron *Electron
			}{
				Electron: &Electron{
					Name: "",
					Lepton: &Lepton{
						Name: "",
						Quark: &Quark{
							Value: 0,
						},
					},
				},
			},
			lookuper: MapLookuper(nil),
		},

		// No init
		{
			name: "noinit/no_init_when_sub_fields_unset",
			target: &struct {
				Sub *struct {
					Field string `env:"FIELD"`
				} `env:",noinit"`
			}{},
			exp: &struct {
				Sub *struct {
					Field string `env:"FIELD"`
				} `env:",noinit"`
			}{
				// Sub struct ptr shouldn't be initiated because the 'Field' is not set
				// in the lookuper.
				Sub: nil,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "noinit/no_init_when_sub_sub_fields_unset",
			target: &struct {
				Lepton *Lepton `env:",noinit"`
			}{},
			exp: &struct {
				Lepton *Lepton `env:",noinit"`
			}{
				// Sub-sub fields should not be initiaized when no value is given.
				Lepton: nil,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "noinit/no_init_from_parent",
			target: &struct {
				Electron *Electron `env:"FIELD,noinit"`
			}{},
			exp: &struct {
				Electron *Electron `env:"FIELD,noinit"`
			}{
				Electron: nil,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "noinit/no_init_when_decoder",
			target: &struct {
				Parent *struct {
					Field url.URL
				} `env:",noinit"`
			}{},
			exp: &struct {
				Parent *struct {
					Field url.URL
				} `env:",noinit"`
			}{
				// Parent should be nil.
				Parent: nil,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "noinit/init_when_sub_fields_set",
			target: &struct {
				Sub *struct {
					Field string `env:"FIELD"`
				} `env:",noinit"`
			}{},
			exp: &struct {
				Sub *struct {
					Field string `env:"FIELD"`
				} `env:",noinit"`
			}{
				// Sub struct ptr should be initiated because the 'Field' is set in the
				// lookuper.
				Sub: &struct {
					Field string `env:"FIELD"`
				}{
					Field: "banana",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "banana",
			}),
		},
		{
			name: "noinit/init_when_sub_sub_fields_set",
			target: &struct {
				Lepton *Lepton `env:",noinit"`
			}{},
			exp: &struct {
				Lepton *Lepton `env:",noinit"`
			}{
				// Sub-sub fields should be initiaized when a value is given.
				Lepton: &Lepton{
					Quark: &Quark{
						Value: 5,
					},
				},
			},
			lookuper: MapLookuper(map[string]string{
				"QUARK_VALUE": "5",
			}),
		},
		{
			name: "noinit/non_struct_ptr",
			target: &struct {
				Field1 *string  `env:"FIELD1, noinit"`
				Field2 *int     `env:"FIELD2, noinit"`
				Field3 *float64 `env:"FIELD3, noinit"`
				Field4 *bool    `env:"FIELD4"`
			}{},
			exp: &struct {
				Field1 *string  `env:"FIELD1, noinit"`
				Field2 *int     `env:"FIELD2, noinit"`
				Field3 *float64 `env:"FIELD3, noinit"`
				Field4 *bool    `env:"FIELD4"`
			}{
				// The pointer fields that had a value should initialize, but the unset
				// values should remain nil, iff they are set to noinit.
				Field1: ptrTo("banana"),
				Field2: ptrTo(int(5)),
				Field3: nil,
				Field4: ptrTo(false),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD1": "banana",
				"FIELD2": "5",
			}),
		},
		{
			name: "noinit/map",
			target: &struct {
				Field map[string]string `env:"FIELD, noinit"`
			}{},
			exp: &struct {
				Field map[string]string `env:"FIELD, noinit"`
			}{
				Field: nil,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "noinit/slice",
			target: &struct {
				Field []string `env:"FIELD, noinit"`
			}{},
			exp: &struct {
				Field []string `env:"FIELD, noinit"`
			}{
				Field: nil,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "noinit/unsafe_pointer",
			target: &struct {
				Field unsafe.Pointer `env:"FIELD, noinit"`
			}{},
			exp: &struct {
				Field unsafe.Pointer `env:"FIELD, noinit"`
			}{
				Field: nil,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "noinit/error_not_ptr",
			target: &struct {
				Field string `env:"FIELD, noinit"`
			}{},
			err:      ErrNoInitNotPtr,
			lookuper: MapLookuper(nil),
		},

		// Inherited configuration
		{
			name: "inherited/delimiter",
			target: &struct {
				Sub *SliceStruct `env:", delimiter=;"`
			}{},
			exp: &struct {
				Sub *SliceStruct `env:", delimiter=;"`
			}{
				Sub: &SliceStruct{
					Field: []string{"foo,", "bar|"},
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo,;bar|",
			}),
		},
		{
			name: "inherited/separator",
			target: &struct {
				Sub *MapStruct `env:", separator=@"`
			}{},
			exp: &struct {
				Sub *MapStruct `env:", separator=@"`
			}{
				Sub: &MapStruct{
					Field: map[string]string{"foo:": "bar=", "zip": "zap"},
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo:@bar=,zip@zap",
			}),
		},
		{
			name: "inherited/delimiter_separator",
			target: &struct {
				Sub *MapStruct `env:", delimiter=;, separator=@"`
			}{},
			exp: &struct {
				Sub *MapStruct `env:", delimiter=;, separator=@"`
			}{
				Sub: &MapStruct{
					Field: map[string]string{"foo,": "bar=", "zip": "zap,"},
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo,@bar=;zip@zap,",
			}),
		},
		{
			name: "inherited/no_init",
			target: &struct {
				Sub *MapStruct `env:", noinit"`
			}{},
			exp: &struct {
				Sub *MapStruct `env:", noinit"`
			}{
				Sub: nil,
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "inherited/overwrite",
			target: &struct {
				Sub *SliceStruct `env:", overwrite"`
			}{
				Sub: &SliceStruct{
					Field: []string{"foo", "bar"},
				},
			},
			exp: &struct {
				Sub *SliceStruct `env:", overwrite"`
			}{
				Sub: &SliceStruct{
					Field: []string{"zip", "zap"},
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "zip,zap",
			}),
		},
		{
			name: "inherited/decodeunset",
			target: &struct {
				Sub *struct {
					Level Level `env:"FIELD, decodeunset"`
				}
			}{},
			exp: &struct {
				Sub *struct {
					Level Level `env:"FIELD, decodeunset"`
				}
			}{
				Sub: &struct {
					Level Level `env:"FIELD, decodeunset"`
				}{
					Level: LevelInfo,
				},
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "inherited/required",
			target: &struct {
				Sub *SliceStruct `env:", required"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrMissingRequired,
		},

		// Global configuration
		{
			name: "global/delimiter",
			target: &struct {
				Sub *SliceStruct
			}{},
			exp: &struct {
				Sub *SliceStruct
			}{
				Sub: &SliceStruct{
					Field: []string{"foo,", "bar|"},
				},
			},
			defDelimiter: ";",
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo,;bar|",
			}),
		},
		{
			name: "global/separator",
			target: &struct {
				Sub *MapStruct
			}{},
			exp: &struct {
				Sub *MapStruct
			}{
				Sub: &MapStruct{
					Field: map[string]string{"foo:": "bar=", "zip": "zap"},
				},
			},
			defSeparator: "@",
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo:@bar=,zip@zap",
			}),
		},
		{
			name: "global/delimiter_separator",
			target: &struct {
				Sub *MapStruct
			}{},
			exp: &struct {
				Sub *MapStruct
			}{
				Sub: &MapStruct{
					Field: map[string]string{"foo,": "bar=", "zip": "zap,"},
				},
			},
			defDelimiter: ";",
			defSeparator: "@",
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo,@bar=;zip@zap,",
			}),
		},
		{
			name: "global/no_init",
			target: &struct {
				Sub *MapStruct
			}{},
			exp: &struct {
				Sub *MapStruct
			}{
				Sub: nil,
			},
			defNoInit: true,
			lookuper:  MapLookuper(nil),
		},
		{
			name: "global/overwrite",
			target: &struct {
				Sub *SliceStruct
			}{
				Sub: &SliceStruct{
					Field: []string{"foo", "bar"},
				},
			},
			exp: &struct {
				Sub *SliceStruct
			}{
				Sub: &SliceStruct{
					Field: []string{"zip", "zap"},
				},
			},
			defOverwrite: true,
			lookuper: MapLookuper(map[string]string{
				"FIELD": "zip,zap",
			}),
		},
		{
			name: "global/decodeunset",
			target: &struct {
				Sub *struct {
					Level Level `env:"LEVEL"`
				}
			}{},
			exp: &struct {
				Sub *struct {
					Level Level `env:"LEVEL"`
				}
			}{
				Sub: &struct {
					Level Level `env:"LEVEL"`
				}{
					Level: LevelInfo,
				},
			},
			defDecodeUnset: true,
			lookuper:       MapLookuper(nil),
		},
		{
			name: "global/required",
			target: &struct {
				Sub *SliceStruct
			}{},
			defRequired: true,
			lookuper:    MapLookuper(nil),
			err:         ErrMissingRequired,
		},

		// Issues - this section is specific to reproducing issues
		{
			// github.com/sethvargo/go-envconfig/issues/13
			name: "process_fields_after_decoder",
			target: &struct {
				Field1 time.Time `env:"FIELD1"`
				Field2 string    `env:"FIELD2"`
			}{},
			exp: &struct {
				Field1 time.Time `env:"FIELD1"`
				Field2 string    `env:"FIELD2"`
			}{
				Field1: time.Unix(0, 0),
				Field2: "bar",
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD1": "1970-01-01T00:00:00Z",
				"FIELD2": "bar",
			}),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/16
			name: "custom_decoder_nested",
			target: &struct {
				Field Base64ByteSlice `env:"FIELD"`
			}{},
			exp: &struct {
				Field Base64ByteSlice `env:"FIELD"`
			}{
				Field: Base64ByteSlice{
					Base64Bytes("foo"),
					Base64Bytes("bar"),
				},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": fmt.Sprintf("%s,%s",
					base64.StdEncoding.EncodeToString([]byte("foo")),
					base64.StdEncoding.EncodeToString([]byte("bar")),
				),
			}),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/28
			name:   "embedded_prefixes/error-keys",
			target: &VCR{},
			errMsg: "VCR_REMOTE_NAME",
			lookuper: MapLookuper(map[string]string{
				"NAME":                   "vcr",
				"VCR_REMOTE_BUTTON_NAME": "button",
			}),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/61
			name: "custom_decoder_overwrite_uses_default",
			target: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{},
			exp: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: LevelError,
			},
			lookuper: MapLookuper(nil),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/61
			name: "custom_decoder_overwrite_unset",
			target: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{},
			exp: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: LevelDebug,
			},
			lookuper: MapLookuper(map[string]string{
				"LEVEL": "debug",
			}),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/61
			name: "custom_decoder_overwrite_existing_value",
			target: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: LevelInfo,
			},
			exp: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: LevelInfo,
			},
			lookuper: MapLookuper(nil),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/61
			name: "custom_decoder_overwrite_existing_value_envvar",
			target: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: LevelInfo,
			},
			exp: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: LevelDebug,
			},
			lookuper: MapLookuper(map[string]string{
				"LEVEL": "debug",
			}),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/64
			name: "custom_decoder_uses_decoder_no_env",
			target: &struct {
				URL *url.URL `env:",noinit"`
			}{},
			exp: &struct {
				URL *url.URL `env:",noinit"`
			}{
				URL: nil,
			},
			lookuper: MapLookuper(nil),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/64
			name: "custom_decoder_uses_decoder_env_with_value",
			target: &struct {
				URL *url.URL `env:"URL"`
			}{},
			exp: &struct {
				URL *url.URL `env:"URL"`
			}{
				URL: &url.URL{
					Scheme: "https",
					Host:   "foo.bar",
				},
			},
			lookuper: MapLookuper(map[string]string{
				"URL": "https://foo.bar",
			}),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/79
			name: "space_delimiter",
			target: &struct {
				Field map[string]string `env:"FIELD,delimiter= "`
			}{},
			exp: &struct {
				Field map[string]string `env:"FIELD,delimiter= "`
			}{
				Field: map[string]string{"foo": "1,2", "bar": "3,4", "zip": "zap:zoo,3"},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo:1,2 bar:3,4 zip:zap:zoo,3",
			}),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/79
			name: "space_separator",
			target: &struct {
				Field map[string]string `env:"FIELD,separator= "`
			}{},
			exp: &struct {
				Field map[string]string `env:"FIELD,separator= "`
			}{
				Field: map[string]string{"foo": "bar", "zip:zap": "zoo:zil"},
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo bar,zip:zap zoo:zil",
			}),
		},
		// LookuperFunc
		{
			name: "lookuperfunc/static",
			target: &struct {
				Field1 string `env:"FIELD1"`
				Field2 string `env:"FIELD2"`
			}{},
			lookuper: LookuperFunc(func(key string) (string, bool) {
				return "foo", true
			}),
			exp: &struct {
				Field1 string `env:"FIELD1"`
				Field2 string `env:"FIELD2"`
			}{
				Field1: "foo",
				Field2: "foo",
			},
		},
		{
			name: "lookuperfunc/branching",
			target: &struct {
				Field1 string `env:"FIELD1"`
				Field2 string `env:"FIELD2"`
			}{},
			lookuper: LookuperFunc(func(key string) (string, bool) {
				if key == "FIELD1" {
					return "foo", true
				}
				return "", false
			}),
			exp: &struct {
				Field1 string `env:"FIELD1"`
				Field2 string `env:"FIELD2"`
			}{
				Field1: "foo",
				Field2: "",
			},
		},
		{
			name: "lookuperfunc/switching",
			target: &struct {
				Field1 string `env:"FIELD1"`
				Field2 string `env:"FIELD2"`
				Field3 string `env:"FIELD3"`
			}{},
			lookuper: LookuperFunc(func(key string) (string, bool) {
				switch key {
				case "FIELD1":
					return "foo", true
				case "FIELD2":
					return "bar", true
				}
				return "", false
			}),
			exp: &struct {
				Field1 string `env:"FIELD1"`
				Field2 string `env:"FIELD2"`
				Field3 string `env:"FIELD3"`
			}{
				Field1: "foo",
				Field2: "bar",
				Field3: "",
			},
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			if err := ProcessWith(ctx, &Config{
				Target:             tc.target,
				Lookuper:           tc.lookuper,
				DefaultDelimiter:   tc.defDelimiter,
				DefaultSeparator:   tc.defSeparator,
				DefaultNoInit:      tc.defNoInit,
				DefaultOverwrite:   tc.defOverwrite,
				DefaultDecodeUnset: tc.defDecodeUnset,
				DefaultRequired:    tc.defRequired,
				Mutators:           tc.mutators,
			}); err != nil {
				if tc.err == nil && tc.errMsg == "" {
					t.Fatal(err)
				}

				if tc.err != nil && !errors.Is(err, tc.err) {
					t.Fatalf("expected \n%+v\n to be \n%+v\n", err, tc.err)
				}

				if got, want := err.Error(), tc.errMsg; want != "" && !strings.Contains(got, want) {
					t.Fatalf("expected \n%+v\n to match \n%+v\n", got, want)
				}

				// There's an error, but it passed all our tests, so return now.
				return
			}

			opts := cmp.AllowUnexported(
				// Custom decoder type
				CustomDecoderType{},

				// Custom standard library interfaces decoder type
				CustomStdLibDecodingType{},

				// Custom decoder type that returns an error
				CustomTypeError{},

				// Anonymous struct with private fields
				struct{ field string }{},
			)
			if diff := cmp.Diff(tc.exp, tc.target, opts); diff != "" {
				t.Fatalf("mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestMustProcess(t *testing.T) {
	cases := []struct {
		name     string
		target   any
		exp      any
		env      map[string]string
		expPanic bool
	}{
		{
			name: "panics_on_error",
			target: &struct {
				Field string `env:"FIELD,required"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,required"`
			}{},
			env:      nil,
			expPanic: true,
		},
		{
			name: "returns_value",
			target: &struct {
				Field string `env:"FIELD,required"`
			}{},
			exp: &struct {
				Field string `env:"FIELD,required"`
			}{
				Field: "value",
			},
			env: map[string]string{
				"FIELD": "value",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			defer func() {
				if r := recover(); r != nil {
					if !tc.expPanic {
						t.Fatal(r)
					}
				} else if tc.expPanic {
					t.Errorf("expected a panic")
				}
			}()

			ctx := context.Background()
			got := MustProcess(ctx, tc.target)

			if got != tc.target {
				t.Errorf("expected result to be the same object as target (%#v, %#v)", got, tc.target)
			}

			if diff := cmp.Diff(tc.exp, tc.target); diff != "" {
				t.Fatalf("mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestValidateEnvName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		exp  bool
	}{
		{
			name: "empty",
			in:   "",
			exp:  false,
		},
		{
			name: "space",
			in:   " ",
			exp:  false,
		},
		{
			name: "digit_start",
			in:   "1FOO",
			exp:  false,
		},
		{
			name: "emoji_start",
			in:   "🚀",
			exp:  false,
		},
		{
			name: "lowercase_start",
			in:   "f",
			exp:  true,
		},
		{
			name: "lowercase",
			in:   "foo",
			exp:  true,
		},
		{
			name: "uppercase_start",
			in:   "F",
			exp:  true,
		},
		{
			name: "uppercase",
			in:   "FOO",
			exp:  true,
		},
		{
			name: "underscore_start",
			in:   "_foo",
			exp:  true,
		},
		{
			name: "emoji_middle",
			in:   "FOO🚀",
			exp:  false,
		},
		{
			name: "space_middle",
			in:   "FOO BAR",
			exp:  false,
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got, want := validateEnvName(tc.in), tc.exp; got != want {
				t.Errorf("expected %q to be %t (got %t)", tc.in, want, got)
			}
		})
	}
}

func ptrTo[T any](i T) *T {
	return &i
}
