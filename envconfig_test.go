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
	DebugLevel Level = 0
	InfoLevel  Level = 5
	ErrorLevel Level = 100
)

func (l *Level) UnmarshalText(text []byte) error {
	switch string(text) {
	case "debug":
		*l = DebugLevel
		return nil
	case "info", "": // default
		*l = InfoLevel
		return nil
	case "error":
		*l = ErrorLevel
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

type Base64ByteSlice []Base64Bytes

func boolPtr(b bool) *bool {
	return &b
}

func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

func TestProcessWith(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    interface{}
		exp      interface{}
		lookuper Lookuper
		mutators []MutatorFunc
		err      error
		errMsg   string
	}{
		// nil pointer
		{
			name:     "nil",
			input:    (*Electron)(nil),
			lookuper: MapLookuper(nil),
			err:      ErrNotStruct,
		},

		// Bool
		{
			name: "bool/true",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
				Field float32 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid float",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "float64/6.022",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
				Field int `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "int8/12",
			input: &struct {
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
			input: &struct {
				Field int8 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "int16/1245",
			input: &struct {
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
			input: &struct {
				Field int16 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "int32/1245",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
				Field int64 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "int64/duration",
			input: &struct {
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
			input: &struct {
				Field *time.Duration `env:"FIELD"`
			}{},
			exp: &struct {
				Field *time.Duration `env:"FIELD"`
			}{
				Field: func() *time.Duration { d := 10 * time.Second; return &d }(),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "10s",
			}),
		},
		{
			name: "int64/duration_error",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
				Field uint `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid uint",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "uint8/12",
			input: &struct {
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
			input: &struct {
				Field uint8 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid uint",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "uint16/1245",
			input: &struct {
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
			input: &struct {
				Field uint16 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid uint",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "uint32/1245",
			input: &struct {
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
			input: &struct {
				Field uint32 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "uint64/1245",
			input: &struct {
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
			input: &struct {
				Field uint64 `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "not a valid int",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "uintptr/1245",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
				Field map[string]string `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
			errMsg: "invalid map item",
		},
		{
			name: "map/key_error",
			input: &struct {
				Field map[bool]bool `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "nope:true",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "map/value_error",
			input: &struct {
				Field map[bool]bool `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "true:nope",
			}),
			errMsg: "invalid syntax",
		},
		{
			name: "map/custom_delimiter",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
				Field string `env:"FIELD, overwrite, default=default"`
			}{},
			exp: &struct {
				Field string `env:"FIELD, overwrite, default=default"`
			}{
				Field: "default",
			},
			lookuper: MapLookuper(nil),
		},

		// Required
		{
			name: "required/present",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
				Field string `env:"FIELD,required"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrMissingRequired,
		},
		{
			name: "required/missing_space",
			input: &struct {
				Field string `env:"FIELD, required"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrMissingRequired,
		},
		{
			name: "required/default",
			input: &struct {
				Field string `env:"FIELD,required,default=foo"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrRequiredAndDefault,
		},
		{
			name: "required/default_space",
			input: &struct {
				Field string `env:"FIELD, required, default=foo"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrRequiredAndDefault,
		},

		// Default
		{
			name: "default/missing",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			name: "default/slice",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
				Field CustomDecoderType `env:"FIELD=foo"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrInvalidEnvvarName,
		},

		// Custom decoding from standard library interfaces
		{
			name: "custom_decoder/gob_decoder",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
				field *CustomDecoderType `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
			err: ErrPrivateField,
		},
		{
			name: "custom_decoder/error",
			input: &struct {
				Field CustomTypeError `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
			errMsg: "broken",
		},
		{
			name: "custom_decoder/called_for_empty_string",
			input: &struct {
				Field CustomTypeError `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "",
			}),
			errMsg: "broken",
		},

		// Expand
		{
			name: "expand/not_default",
			input: &struct {
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
			input: &struct {
				Field *string `env:"FIELD"`
			}{},
			exp: &struct {
				Field *string `env:"FIELD"`
			}{
				Field: stringPtr("foo"),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "pointer_pointer_string",
			input: &struct {
				Field **string `env:"FIELD"`
			}{},
			exp: &struct {
				Field **string `env:"FIELD"`
			}{
				Field: func() **string { ptr := stringPtr("foo"); return &ptr }(),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "pointer_int",
			input: &struct {
				Field *int `env:"FIELD"`
			}{},
			exp: &struct {
				Field *int `env:"FIELD"`
			}{
				Field: intPtr(5),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "5",
			}),
		},
		{
			name: "pointer_map",
			input: &struct {
				Field *map[string]string `env:"FIELD"`
			}{},
			exp: &struct {
				Field *map[string]string `env:"FIELD"`
			}{
				Field: func() *map[string]string {
					m := map[string]string{"foo": "bar"}
					return &m
				}(),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo:bar",
			}),
		},
		{
			name: "pointer_slice",
			input: &struct {
				Field *[]string `env:"FIELD"`
			}{},
			exp: &struct {
				Field *[]string `env:"FIELD"`
			}{
				Field: func() *[]string {
					s := []string{"foo"}
					return &s
				}(),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "pointer_bool",
			input: &struct {
				Field *bool `env:"FIELD"`
			}{},
			exp: &struct {
				Field *bool `env:"FIELD"`
			}{
				Field: boolPtr(true),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "true",
			}),
		},
		{
			name: "pointer_bool_noinit",
			input: &struct {
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
			input: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: boolPtr(false),
			},
			exp: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: boolPtr(false),
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "pointer_bool_default_field_set_env_set",
			input: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: boolPtr(false),
			},
			exp: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: boolPtr(false),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "true",
			}),
		},
		{
			name: "pointer_bool_default_field_unset_env_set",
			input: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{},
			exp: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: boolPtr(false),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "false",
			}),
		},
		{
			name: "pointer_bool_default_field_unset_env_unset",
			input: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{},
			exp: &struct {
				Field *bool `env:"FIELD,default=true"`
			}{
				Field: boolPtr(true),
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "pointer_bool_default_overwrite_field_set_env_unset",
			input: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: boolPtr(false),
			},
			exp: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: boolPtr(false),
			},
			lookuper: MapLookuper(nil),
		},
		{
			name: "pointer_bool_default_overwrite_field_set_env_set",
			input: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: boolPtr(false),
			},
			exp: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: boolPtr(true),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "true",
			}),
		},
		{
			name: "pointer_bool_default_overwrite_field_unset_env_set",
			input: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{},
			exp: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: boolPtr(false),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "false",
			}),
		},
		{
			name: "pointer_bool_default_overwrite_field_unset_env_unset",
			input: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{},
			exp: &struct {
				Field *bool `env:"FIELD,overwrite,default=true"`
			}{
				Field: boolPtr(true),
			},
			lookuper: MapLookuper(nil),
		},

		// Marshallers
		{
			name: "binarymarshaler",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
				Field *time.Time `env:"FIELD"`
			}{},
			exp: &struct {
				Field *time.Time `env:"FIELD"`
			}{
				Field: func() *time.Time {
					t := time.Unix(0, 0)
					return &t
				}(),
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
			input: &struct {
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
			input: &struct {
				Field *time.Time `env:"FIELD"`
			}{},
			exp: &struct {
				Field *time.Time `env:"FIELD"`
			}{
				Field: func() *time.Time {
					t := time.Unix(0, 0)
					return &t
				}(),
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
			input: &struct {
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
			input: &struct {
				Field *time.Time `env:"FIELD"`
			}{},
			exp: &struct {
				Field *time.Time `env:"FIELD"`
			}{
				Field: func() *time.Time {
					t := time.Unix(0, 0)
					return &t
				}(),
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
			input: &struct {
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
			mutators: []MutatorFunc{valueMutatorFunc},
		},
		{
			name: "mutate/stops",
			input: &struct {
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
			mutators: []MutatorFunc{
				func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return "value-1", true, nil
				},
				func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return "value-2", true, nil
				},
			},
		},
		{
			name: "mutate/original_and_resolved_keys",
			input: &struct {
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
			mutators: []MutatorFunc{
				func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return fmt.Sprintf("oKey:%s, rKey:%s", oKey, rKey), false, nil
				},
			},
		},
		{
			name: "mutate/original_and_current_values",
			input: &struct {
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
			mutators: []MutatorFunc{
				func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return "new-value", false, nil
				},
				func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return fmt.Sprintf("oVal:%s, cVal:%s", oVal, cVal), false, nil
				},
			},
		},
		{
			name: "mutate/halts_error",
			input: &struct {
				Field string `env:"FIELD"`
			}{},
			exp: &struct {
				Field string `env:"FIELD"`
			}{},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "",
			}),
			mutators: []MutatorFunc{
				func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return "", false, fmt.Errorf("error 1")
				},
				func(_ context.Context, oKey, rKey, oVal, cVal string) (string, bool, error) {
					return "", false, fmt.Errorf("error 2")
				},
			},
			errMsg: "error 1",
		},

		// Nesting
		{
			name:  "nested_pointer_structs",
			input: &Electron{},
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
			name:  "nested_structs",
			input: &Sandwich{},
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
			name:  "nested_mutation",
			input: &Sandwich{},
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
			mutators: []MutatorFunc{valueMutatorFunc},
		},

		// Overwriting
		{
			name: "no_overwrite/structs",
			input: &Electron{
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
			input: &struct {
				Field *string `env:"FIELD"`
			}{
				Field: func() *string { s := "bar"; return &s }(),
			},
			exp: &struct {
				Field *string `env:"FIELD"`
			}{
				Field: func() *string { s := "bar"; return &s }(),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},
		{
			name: "no_overwrite/pointers_pointers",
			input: &struct {
				Field **string `env:"FIELD"`
			}{
				Field: func() **string {
					s := "bar"
					ptr := &s
					return &ptr
				}(),
			},
			exp: &struct {
				Field **string `env:"FIELD"`
			}{
				Field: func() **string {
					s := "bar"
					ptr := &s
					return &ptr
				}(),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD": "foo",
			}),
		},

		// Unknown options
		{
			name: "unknown_options",
			input: &struct {
				Field string `env:"FIELD,cookies"`
			}{},
			lookuper: MapLookuper(nil),
			err:      ErrUnknownOption,
		},

		// Lookup prefixes
		{
			name: "lookup_prefixes",
			input: &struct {
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
			name:  "embedded_prefixes/pointers",
			input: &TV{},
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
			name:  "embedded_prefixes/values",
			input: &VCR{},
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
			name:  "embedded_prefixes/defaults",
			input: &TV{},
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
			input: &struct {
				Field string `env:",prefix=FIELD_"`
			}{},
			err:      ErrPrefixNotStruct,
			lookuper: MapLookuper(nil),
		},

		// Init (default behavior)
		{
			name: "init/basic",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
				Field1: func() *string { x := "banana"; return &x }(),
				Field2: func() *int { x := 5; return &x }(),
				Field3: nil,
				Field4: func() *bool { x := false; return &x }(),
			},
			lookuper: MapLookuper(map[string]string{
				"FIELD1": "banana",
				"FIELD2": "5",
			}),
		},
		{
			name: "noinit/map",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
			input: &struct {
				Field string `env:"FIELD, noinit"`
			}{},
			err:      ErrNoInitNotPtr,
			lookuper: MapLookuper(nil),
		},

		// Issues - this section is specific to reproducing issues
		{
			// github.com/sethvargo/go-envconfig/issues/13
			name: "process_fields_after_decoder",
			input: &struct {
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
			input: &struct {
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
			input:  &VCR{},
			errMsg: "VCR_REMOTE_NAME",
			lookuper: MapLookuper(map[string]string{
				"NAME":                   "vcr",
				"VCR_REMOTE_BUTTON_NAME": "button",
			}),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/61
			name: "custom_decoder_overwrite_uses_default",
			input: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{},
			exp: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: ErrorLevel,
			},
			lookuper: MapLookuper(nil),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/61
			name: "custom_decoder_overwrite_unset",
			input: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{},
			exp: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: DebugLevel,
			},
			lookuper: MapLookuper(map[string]string{
				"LEVEL": "debug",
			}),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/61
			name: "custom_decoder_overwrite_existing_value",
			input: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: InfoLevel,
			},
			exp: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: InfoLevel,
			},
			lookuper: MapLookuper(nil),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/61
			name: "custom_decoder_overwrite_existing_value_envvar",
			input: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: InfoLevel,
			},
			exp: &struct {
				Level Level `env:"LEVEL,overwrite,default=error"`
			}{
				Level: DebugLevel,
			},
			lookuper: MapLookuper(map[string]string{
				"LEVEL": "debug",
			}),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/64
			name: "custom_decoder_uses_decoder_no_env",
			input: &struct {
				URL *url.URL
			}{},
			exp: &struct {
				URL *url.URL
			}{
				URL: &url.URL{},
			},
			lookuper: MapLookuper(nil),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/64
			name: "custom_decoder_uses_decoder_env_no_value",
			input: &struct {
				URL *url.URL `env:"URL"`
			}{},
			exp: &struct {
				URL *url.URL `env:"URL"`
			}{
				URL: &url.URL{},
			},
			lookuper: MapLookuper(nil),
		},
		{
			// https://github.com/sethvargo/go-envconfig/issues/64
			name: "custom_decoder_uses_decoder_env_with_value",
			input: &struct {
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
			input: &struct {
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
			input: &struct {
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
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			if err := ProcessWith(ctx, tc.input, tc.lookuper, tc.mutators...); err != nil {
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
			if diff := cmp.Diff(tc.exp, tc.input, opts); diff != "" {
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

func TestExtractDefaults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		input  interface{}
		exp    interface{}
		err    error
		errMsg string
	}{
		{
			name:  "nil",
			input: (*Electron)(nil),
			err:   ErrNotStruct,
		},
		{
			name: "extracts",
			input: &struct {
				Field1 string `env:"FIELD, default=8080"`
				Field2 string `env:"FIELD"`
			}{},
			exp: &struct {
				Field1 string `env:"FIELD, default=8080"`
				Field2 string `env:"FIELD"`
			}{
				Field1: "8080",
				Field2: "",
			},
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			if err := ExtractDefaults(ctx, tc.input); err != nil {
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
			if diff := cmp.Diff(tc.exp, tc.input, opts); diff != "" {
				t.Fatalf("mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
