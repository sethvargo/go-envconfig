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

// Package envconfig populates struct fields based on environment variable
// values (or anything that responds to "Lookup"). Structs declare their
// environment dependencies using the `env` tag with the key being the name of
// the environment variable, case sensitive.
//
//     type MyStruct struct {
//         A string `env:"A"` // resolves A to $A
//         B string `env:"B,required"` // resolves B to $B, errors if $B is unset
//         C string `env:"C,default=foo"` // resolves C to $C, defaults to "foo"
//
//         D string `env:"D,required,default=foo"` // error, cannot be required and default
//         E string `env:""` // error, must specify key
//     }
//
// All built-in types are supported except Func and Chan. If you need to define
// a custom decoder, implement Decoder:
//
//     type MyStruct struct {
//         field string
//     }
//
//     func (v *MyStruct) EnvDecode(val string) error {
//         v.field = fmt.Sprintf("PREFIX-%s", val)
//         return nil
//     }
//
// In the environment, slices are specified as comma-separated values:
//
//     export MYVAR="a,b,c,d" // []string{"a", "b", "c", "d"}
//
// In the environment, maps are specified as comma-separated key:value pairs:
//
//     export MYVAR="a:b,c:d" // map[string]string{"a":"b", "c":"d"}
//
// If you need to modify environment variable values before processing, you can
// specify a custom mutator:
//
//     type Config struct {
//         Password `env:"PASSWORD_SECRET"`
//     }
//
//     func resolveSecretFunc(ctx context.Context, key, value string) (string, error) {
//         if strings.HasPrefix(value, "secret://") {
//             return secretmanager.Resolve(ctx, value) // example
//         }
//         return value, nil
//     }
//
//     var config Config
//     ProcessWith(&config, OsLookuper(), resolveSecretFunc)
//
package envconfig

import (
	"context"
	"encoding"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	envTag = "env"

	optDefault   = "default="
	optDelimiter = "delimiter="
	optNoInit    = "noinit"
	optOverwrite = "overwrite"
	optPrefix    = "prefix="
	optRequired  = "required"
	optSeparator = "separator="

	defaultDelimiter = ","
	defaultSeparator = ":"
)

var envvarNameRe = regexp.MustCompile(`\A[a-zA-Z_][a-zA-Z0-9_]*\z`)

// Error is a custom error type for errors returned by envconfig.
type Error string

// Error implements error.
func (e Error) Error() string {
	return string(e)
}

const (
	ErrInvalidEnvvarName  = Error("invalid environment variable name")
	ErrInvalidMapItem     = Error("invalid map item")
	ErrLookuperNil        = Error("lookuper cannot be nil")
	ErrMissingKey         = Error("missing key")
	ErrMissingRequired    = Error("missing required value")
	ErrNoInitNotPtr       = Error("field must be a pointer to have noinit")
	ErrNotPtr             = Error("input must be a pointer")
	ErrNotStruct          = Error("input must be a struct")
	ErrPrefixNotStruct    = Error("prefix is only valid on struct types")
	ErrPrivateField       = Error("cannot parse private fields")
	ErrRequiredAndDefault = Error("field cannot be required and have a default value")
	ErrUnknownOption      = Error("unknown option")
)

// Lookuper is an interface that provides a lookup for a string-based key.
type Lookuper interface {
	// Lookup searches for the given key and returns the corresponding string
	// value. If a value is found, it returns the value and true. If a value is
	// not found, it returns the empty string and false.
	Lookup(key string) (string, bool)
}

// osLookuper looks up environment configuration from the local environment.
type osLookuper struct{}

// Verify implements interface.
var _ Lookuper = (*osLookuper)(nil)

func (o *osLookuper) Lookup(key string) (string, bool) {
	return os.LookupEnv(key)
}

// OsLookuper returns a lookuper that uses the environment (os.LookupEnv) to
// resolve values.
func OsLookuper() Lookuper {
	return new(osLookuper)
}

type mapLookuper map[string]string

var _ Lookuper = (*mapLookuper)(nil)

func (m mapLookuper) Lookup(key string) (string, bool) {
	v, ok := m[key]
	return v, ok
}

// MapLookuper looks up environment configuration from a provided map. This is
// useful for testing, especially in parallel, since it does not require you to
// mutate the parent environment (which is stateful).
func MapLookuper(m map[string]string) Lookuper {
	return mapLookuper(m)
}

type multiLookuper struct {
	ls []Lookuper
}

var _ Lookuper = (*multiLookuper)(nil)

func (m *multiLookuper) Lookup(key string) (string, bool) {
	for _, l := range m.ls {
		if v, ok := l.Lookup(key); ok {
			return v, true
		}
	}
	return "", false
}

// PrefixLookuper looks up environment configuration using the specified prefix.
// This is useful if you want all your variables to start with a particular
// prefix like "MY_APP_".
func PrefixLookuper(prefix string, l Lookuper) Lookuper {
	if typ, ok := l.(*prefixLookuper); ok {
		return &prefixLookuper{prefix: typ.prefix + prefix, l: typ.l}
	}
	return &prefixLookuper{prefix: prefix, l: l}
}

type prefixLookuper struct {
	l      Lookuper
	prefix string
}

func (p *prefixLookuper) Lookup(key string) (string, bool) {
	return p.l.Lookup(p.prefix + key)
}

// MultiLookuper wraps a collection of lookupers. It does not combine them, and
// lookups appear in the order in which they are provided to the initializer.
func MultiLookuper(lookupers ...Lookuper) Lookuper {
	return &multiLookuper{ls: lookupers}
}

// Decoder is an interface that custom types/fields can implement to control how
// decoding takes place. For example:
//
//     type MyType string
//
//     func (mt MyType) EnvDecode(val string) error {
//         return "CUSTOM-"+val
//     }
//
type Decoder interface {
	EnvDecode(val string) error
}

// MutatorFunc is a function that mutates a given value before it is passed
// along for processing. This is useful if you want to mutate the environment
// variable value before it's converted to the proper type.
type MutatorFunc func(ctx context.Context, k, v string) (string, error)

// options are internal options for decoding.
type options struct {
	Default   string
	Delimiter string
	Prefix    string
	Separator string
	NoInit    bool
	Overwrite bool
	Required  bool
}

// Process processes the struct using the environment. See ProcessWith for a
// more customizable version.
func Process(ctx context.Context, i interface{}) error {
	return ProcessWith(ctx, i, OsLookuper())
}

// ProcessWith processes the given interface with the given lookuper. See the
// package-level documentation for specific examples and behaviors.
func ProcessWith(ctx context.Context, i interface{}, l Lookuper, fns ...MutatorFunc) error {
	return processWith(ctx, i, l, false, fns...)
}

// processWith is a helper that captures whether the parent wanted
// initialization.
func processWith(ctx context.Context, i interface{}, l Lookuper, parentNoInit bool, fns ...MutatorFunc) error {
	if l == nil {
		return ErrLookuperNil
	}

	v := reflect.ValueOf(i)
	if v.Kind() != reflect.Ptr {
		return ErrNotPtr
	}

	e := v.Elem()
	if e.Kind() != reflect.Struct {
		return ErrNotStruct
	}

	t := e.Type()

	for i := 0; i < t.NumField(); i++ {
		ef := e.Field(i)
		tf := t.Field(i)
		tag := tf.Tag.Get(envTag)

		if !ef.CanSet() {
			if tag != "" {
				// There's an "env" tag on a private field, we can't alter it, and it's
				// likely a mistake. Return an error so the user can handle.
				return fmt.Errorf("%s: %w", tf.Name, ErrPrivateField)
			}

			// Otherwise continue to the next field.
			continue
		}

		// Parse the key and options.
		key, opts, err := keyAndOpts(tag)
		if err != nil {
			return fmt.Errorf("%s: %w", tf.Name, err)
		}

		// NoInit is only permitted on pointers.
		if opts.NoInit && ef.Kind() != reflect.Ptr {
			return fmt.Errorf("%s: %w", tf.Name, ErrNoInitNotPtr)
		}

		isNilStructPtr := false
		setNilStruct := func(v reflect.Value) {
			origin := e.Field(i)
			if isNilStructPtr {
				empty := reflect.New(origin.Type().Elem()).Interface()
				// If a struct (after traversal) equals to the empty value, it means
				// nothing was changed in any sub-fields. With the noinit opt, we skip
				// setting the empty value to the original struct pointer (aka. keep it
				// nil).
				if !reflect.DeepEqual(v.Interface(), empty) || (!opts.NoInit && !parentNoInit) {
					origin.Set(v)
				}
			}
		}

		// Initialize pointer structs.
		for ef.Kind() == reflect.Ptr {
			if ef.IsNil() {
				if ef.Type().Elem().Kind() != reflect.Struct {
					// This is a nil pointer to something that isn't a struct, like
					// *string. Move along.
					break
				}

				isNilStructPtr = true
				// Use an empty struct of the type so we can traverse.
				ef = reflect.New(ef.Type().Elem()).Elem()

			} else {
				ef = ef.Elem()
			}
		}

		// Special case handle structs. This has to come after the value resolution in
		// case the struct has a custom decoder.
		if ef.Kind() == reflect.Struct {
			for ef.CanAddr() {
				ef = ef.Addr()
			}

			// Lookup the value, ignoring an error if the key isn't defined. This is
			// required for nested structs that don't declare their own `env` keys,
			// but have internal fields with an `env` defined.
			val, found, usedDefault, err := lookup(key, opts, l)
			if err != nil && !errors.Is(err, ErrMissingKey) {
				return fmt.Errorf("%s: %w", tf.Name, err)
			}

			if found || usedDefault {
				if ok, err := processAsDecoder(val, ef); ok {
					if err != nil {
						return err
					}

					setNilStruct(ef)
					continue
				}
			}

			plu := l
			if opts.Prefix != "" {
				plu = PrefixLookuper(opts.Prefix, l)
			}

			if err := processWith(ctx, ef.Interface(), plu, opts.NoInit, fns...); err != nil {
				return fmt.Errorf("%s: %w", tf.Name, err)
			}

			setNilStruct(ef)
			continue
		}

		// It's invalid to have a prefix on a non-struct field.
		if opts.Prefix != "" {
			return ErrPrefixNotStruct
		}

		// Stop processing if there's no env tag (this comes after nested parsing),
		// in case there's an env tag in an embedded struct.
		if tag == "" {
			continue
		}

		// The field already has a non-zero value and overwrite is false, do not
		// overwrite.
		if !ef.IsZero() && !opts.Overwrite {
			continue
		}

		val, found, usedDefault, err := lookup(key, opts, l)
		if err != nil {
			return fmt.Errorf("%s: %w", tf.Name, err)
		}

		// If the field already has a non-zero value and there was no value directly
		// specified, do not overwrite the existing field. We only want to overwrite
		// when the envvar was provided directly.
		if !ef.IsZero() && !found {
			continue
		}

		// Apply any mutators. Mutators are applied after the lookup, but before any
		// type conversions. They always resolve to a string (or error), so we don't
		// call mutators when the environment variable was not set.
		if found || usedDefault {
			for _, fn := range fns {
				if fn != nil {
					val, err = fn(ctx, key, val)
					if err != nil {
						return fmt.Errorf("%s: %w", tf.Name, err)
					}
				}
			}
		}

		// If Delimiter is not defined set it to ","
		if opts.Delimiter == "" {
			opts.Delimiter = defaultDelimiter
		}

		// If Separator is not defined set it to ":"
		if opts.Separator == "" {
			opts.Separator = defaultSeparator
		}

		// Set value.
		if err := processField(val, ef, opts.Delimiter, opts.Separator, opts.NoInit); err != nil {
			return fmt.Errorf("%s(%q): %w", tf.Name, val, err)
		}
	}

	return nil
}

// keyAndOpts parses the given tag value (e.g. env:"foo,required") and
// returns the key name and options as a list.
func keyAndOpts(tag string) (string, *options, error) {
	parts := strings.Split(tag, ",")
	key, tagOpts := strings.TrimSpace(parts[0]), parts[1:]

	if key != "" && !envvarNameRe.MatchString(key) {
		return "", nil, fmt.Errorf("%q: %w ", key, ErrInvalidEnvvarName)
	}

	var opts options

LOOP:
	for i, o := range tagOpts {
		o = strings.TrimSpace(o)
		switch {
		case o == optOverwrite:
			opts.Overwrite = true
		case o == optRequired:
			opts.Required = true
		case o == optNoInit:
			opts.NoInit = true
		case strings.HasPrefix(o, optPrefix):
			opts.Prefix = strings.TrimPrefix(o, optPrefix)
		case strings.HasPrefix(o, optDelimiter):
			opts.Delimiter = strings.TrimPrefix(o, optDelimiter)
		case strings.HasPrefix(o, optSeparator):
			opts.Separator = strings.TrimPrefix(o, optSeparator)
		case strings.HasPrefix(o, optDefault):
			// If a default value was given, assume everything after is the provided
			// value, including comma-seprated items.
			o = strings.TrimLeft(strings.Join(tagOpts[i:], ","), " ")
			opts.Default = strings.TrimPrefix(o, optDefault)
			break LOOP
		default:
			return "", nil, fmt.Errorf("%q: %w", o, ErrUnknownOption)
		}
	}

	return key, &opts, nil
}

// lookup looks up the given key using the provided Lookuper and options. The
// first boolean parameter indicates whether the value was found in the
// lookuper. The second boolean parameter indicates whether the default value
// was used.
func lookup(key string, opts *options, l Lookuper) (string, bool, bool, error) {
	if key == "" {
		// The struct has something like `env:",required"`, which is likely a
		// mistake. We could try to infer the envvar from the field name, but that
		// feels too magical.
		return "", false, false, ErrMissingKey
	}

	if opts.Required && opts.Default != "" {
		// Having a default value on a required value doesn't make sense.
		return "", false, false, ErrRequiredAndDefault
	}

	// Lookup value.
	val, found := l.Lookup(key)
	if !found {
		if opts.Required {
			if pl, ok := l.(*prefixLookuper); ok {
				key = pl.prefix + key
			}

			return "", false, false, fmt.Errorf("%w: %s", ErrMissingRequired, key)
		}

		if opts.Default != "" {
			// Expand the default value. This allows for a default value that maps to
			// a different variable.
			val = os.Expand(opts.Default, func(i string) string {
				s, ok := l.Lookup(i)
				if ok {
					return s
				}
				return ""
			})

			return val, false, true, nil
		}
	}

	return val, found, false, nil
}

// processAsDecoder processes the given value as a decoder or custom
// unmarshaller.
func processAsDecoder(v string, ef reflect.Value) (bool, error) {
	// Keep a running error. It's possible that a property might implement
	// multiple decoders, and we don't know *which* decoder will succeed. If we
	// get through all of them, we'll return the most recent error.
	var imp bool
	var err error

	// Resolve any pointers.
	for ef.CanAddr() {
		ef = ef.Addr()
	}

	if ef.CanInterface() {
		iface := ef.Interface()

		// If a developer chooses to implement the Decoder interface on a type,
		// never attempt to use other decoders in case of failure. EnvDecode's
		// decoding logic is "the right one", and the error returned (if any)
		// is the most specific we can get.
		if dec, ok := iface.(Decoder); ok {
			imp = true
			err = dec.EnvDecode(v)
			return imp, err
		}

		if tu, ok := iface.(encoding.TextUnmarshaler); ok {
			imp = true
			if err = tu.UnmarshalText([]byte(v)); err == nil {
				return imp, nil
			}
		}

		if tu, ok := iface.(json.Unmarshaler); ok {
			imp = true
			if err = tu.UnmarshalJSON([]byte(v)); err == nil {
				return imp, nil
			}
		}

		if tu, ok := iface.(encoding.BinaryUnmarshaler); ok {
			imp = true
			if err = tu.UnmarshalBinary([]byte(v)); err == nil {
				return imp, nil
			}
		}

		if tu, ok := iface.(gob.GobDecoder); ok {
			imp = true
			if err = tu.GobDecode([]byte(v)); err == nil {
				return imp, nil
			}
		}
	}

	return imp, err
}

func processField(v string, ef reflect.Value, delimiter, separator string, noInit bool) error {
	// If the input value is empty and initialization is skipped, do nothing.
	if v == "" && noInit {
		return nil
	}

	// Handle pointers and uninitialized pointers.
	for ef.Type().Kind() == reflect.Ptr {
		if ef.IsNil() {
			ef.Set(reflect.New(ef.Type().Elem()))
		}
		ef = ef.Elem()
	}

	tf := ef.Type()
	tk := tf.Kind()

	// Handle existing decoders.
	if ok, err := processAsDecoder(v, ef); ok {
		return err
	}

	// We don't check if the value is empty earlier, because the user might want
	// to define a custom decoder and treat the empty variable as a special case.
	// However, if we got this far, none of the remaining parsers will succeed, so
	// bail out now.
	if v == "" {
		return nil
	}

	switch tk {
	case reflect.Bool:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		ef.SetBool(b)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(v, tf.Bits())
		if err != nil {
			return err
		}
		ef.SetFloat(f)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		i, err := strconv.ParseInt(v, 0, tf.Bits())
		if err != nil {
			return err
		}
		ef.SetInt(i)
	case reflect.Int64:
		// Special case time.Duration values.
		if tf.PkgPath() == "time" && tf.Name() == "Duration" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			ef.SetInt(int64(d))
		} else {
			i, err := strconv.ParseInt(v, 0, tf.Bits())
			if err != nil {
				return err
			}
			ef.SetInt(i)
		}
	case reflect.String:
		ef.SetString(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		i, err := strconv.ParseUint(v, 0, tf.Bits())
		if err != nil {
			return err
		}
		ef.SetUint(i)

	case reflect.Interface:
		return fmt.Errorf("cannot decode into interfaces")

	// Maps
	case reflect.Map:
		vals := strings.Split(v, delimiter)
		mp := reflect.MakeMapWithSize(tf, len(vals))
		for _, val := range vals {
			pair := strings.SplitN(val, separator, 2)
			if len(pair) < 2 {
				return fmt.Errorf("%s: %w", val, ErrInvalidMapItem)
			}
			mKey, mVal := strings.TrimSpace(pair[0]), strings.TrimSpace(pair[1])

			k := reflect.New(tf.Key()).Elem()
			if err := processField(mKey, k, delimiter, separator, noInit); err != nil {
				return fmt.Errorf("%s: %w", mKey, err)
			}

			v := reflect.New(tf.Elem()).Elem()
			if err := processField(mVal, v, delimiter, separator, noInit); err != nil {
				return fmt.Errorf("%s: %w", mVal, err)
			}

			mp.SetMapIndex(k, v)
		}
		ef.Set(mp)

	// Slices
	case reflect.Slice:
		// Special case: []byte
		if tf.Elem().Kind() == reflect.Uint8 {
			ef.Set(reflect.ValueOf([]byte(v)))
		} else {
			vals := strings.Split(v, delimiter)
			s := reflect.MakeSlice(tf, len(vals), len(vals))
			for i, val := range vals {
				val = strings.TrimSpace(val)
				if err := processField(val, s.Index(i), delimiter, separator, noInit); err != nil {
					return fmt.Errorf("%s: %w", val, err)
				}
			}
			ef.Set(s)
		}
	}

	return nil
}
