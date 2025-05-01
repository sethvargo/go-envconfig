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
// environment dependencies using the "env" tag with the key being the name of
// the environment variable, case sensitive.
//
//	type MyStruct struct {
//	  A string `env:"A"` // resolves A to $A
//	  B string `env:"B,required"` // resolves B to $B, errors if $B is unset
//	  C string `env:"C,default=foo"` // resolves C to $C, defaults to "foo"
//
//	  D string `env:"D,required,default=foo"` // error, cannot be required and default
//	  E string `env:""` // error, must specify key
//	}
//
// All built-in types are supported except Func and Chan. If you need to define
// a custom decoder, implement Decoder:
//
//	type MyStruct struct {
//	  field string
//	}
//
//	func (v *MyStruct) EnvDecode(val string) error {
//	  v.field = fmt.Sprintf("PREFIX-%s", val)
//	  return nil
//	}
//
// In the environment, slices are specified as comma-separated values:
//
//	export MYVAR="a,b,c,d" // []string{"a", "b", "c", "d"}
//
// In the environment, maps are specified as comma-separated key:value pairs:
//
//	export MYVAR="a:b,c:d" // map[string]string{"a":"b", "c":"d"}
//
// For more configuration options and examples, see the documentation.
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
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	envTag = "env"

	optDecodeUnset = "decodeunset"
	optDefault     = "default="
	optDelimiter   = "delimiter="
	optNoInit      = "noinit"
	optOverwrite   = "overwrite"
	optPrefix      = "prefix="
	optRequired    = "required"
	optSeparator   = "separator="
)

// internalError is a custom error type for errors returned by envconfig.
type internalError string

// Error implements error.
func (e internalError) Error() string {
	return string(e)
}

const (
	ErrInvalidEnvvarName  = internalError("invalid environment variable name")
	ErrInvalidMapItem     = internalError("invalid map item")
	ErrLookuperNil        = internalError("lookuper cannot be nil")
	ErrMissingKey         = internalError("missing key")
	ErrMissingRequired    = internalError("missing required value")
	ErrNoInitNotPtr       = internalError("field must be a pointer to have noinit")
	ErrNotPtr             = internalError("input must be a pointer")
	ErrNotStruct          = internalError("input must be a struct")
	ErrPrefixNotStruct    = internalError("prefix is only valid on struct types")
	ErrPrivateField       = internalError("cannot parse private fields")
	ErrRequiredAndDefault = internalError("field cannot be required and have a default value")
	ErrUnknownOption      = internalError("unknown option")
)

// Lookuper is an interface that provides a lookup for a string-based key.
type Lookuper interface {
	// Lookup searches for the given key and returns the corresponding string
	// value. If a value is found, it returns the value and true. If a value is
	// not found, it returns the empty string and false.
	Lookup(key string) (string, bool)
}

var _ Lookuper = (LookuperFunc)(nil)

// LookuperFunc implements the [Lookuper] interface and provides a quick way to
// create an anonymous function that performs a lookup for a string-based key.
type LookuperFunc func(key string) (string, bool)

// Lookup implements [Lookuper].
func (l LookuperFunc) Lookup(key string) (string, bool) {
	return l(key)
}

// osLookuper looks up environment configuration from the local environment.
type osLookuper struct{}

// Verify implements interface.
var _ Lookuper = (*osLookuper)(nil)

func (o *osLookuper) Lookup(key string) (string, bool) {
	return os.LookupEnv(key)
}

// OsLookuper returns a lookuper that uses the environment ([os.LookupEnv]) to
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
	return p.l.Lookup(p.Key(key))
}

func (p *prefixLookuper) Key(key string) string {
	return p.prefix + key
}

func (p *prefixLookuper) Unwrap() Lookuper {
	l := p.l
	for v, ok := l.(unwrappableLookuper); ok; {
		l = v.Unwrap()
	}
	return l
}

// unwrappableLookuper is a lookuper that can return the underlying lookuper.
type unwrappableLookuper interface {
	Unwrap() Lookuper
}

// MultiLookuper wraps a collection of lookupers. It does not combine them, and
// lookups appear in the order in which they are provided to the initializer.
func MultiLookuper(lookupers ...Lookuper) Lookuper {
	return &multiLookuper{ls: lookupers}
}

// keyedLookuper is an extension to the [Lookuper] interface that returns the
// underlying key (used by the [PrefixLookuper] or custom implementations).
type keyedLookuper interface {
	Key(key string) string
}

// Decoder is the legacy implementation of [DecoderCtx], but it does not accept
// a context as the first parameter to `EnvDecode`. Please use [DecoderCtx]
// instead, as this will be removed in a future release.
type Decoder interface {
	EnvDecode(val string) error
}

// DecoderCtx is an interface that custom types/fields can implement to control
// how decoding takes place. For example:
//
//	type MyType string
//
//	func (mt MyType) EnvDecode(ctx context.Context, val string) error {
//	    return "CUSTOM-"+val
//	}
type DecoderCtx interface {
	EnvDecode(ctx context.Context, val string) error
}

// options are internal options for decoding.
type options struct {
	Default     string
	Delimiter   string
	Prefix      string
	Separator   string
	NoInit      bool
	Overwrite   bool
	DecodeUnset bool
	Required    bool
}

// Config represent inputs to the envconfig decoding.
type Config struct {
	// Target is the destination structure to decode. This value is required, and
	// it must be a pointer to a struct.
	Target any

	// Lookuper is the lookuper implementation to use. If not provided, it
	// defaults to the OS Lookuper.
	Lookuper Lookuper

	// DefaultDelimiter is the default value to use for the delimiter in maps and
	// slices. This can be overridden on a per-field basis, which takes
	// precedence. The default value is ",".
	DefaultDelimiter string

	// DefaultSeparator is the default value to use for the separator in maps.
	// This can be overridden on a per-field basis, which takes precedence. The
	// default value is ":".
	DefaultSeparator string

	// DefaultNoInit is the default value for skipping initialization of
	// unprovided fields. The default value is false (deeply initialize all
	// fields and nested structs).
	DefaultNoInit bool

	// DefaultOverwrite is the default value for overwriting an existing value set
	// on the struct before processing. The default value is false.
	DefaultOverwrite bool

	// DefaultDecodeUnset is the default value for running decoders even when no
	// value was given for the environment variable.
	DefaultDecodeUnset bool

	// DefaultRequired is the default value for marking a field as required. The
	// default value is false.
	DefaultRequired bool

	// Mutators is an optional list of mutators to apply to lookups.
	Mutators []Mutator
}

// Process decodes the struct using values from environment variables. See
// [ProcessWith] for a more customizable version.
//
// As a special case, if the input for the target is a [*Config], then this
// function will call [ProcessWith] using the provided config, with any mutation
// appended.
func Process(ctx context.Context, i any, mus ...Mutator) error {
	if v, ok := i.(*Config); ok {
		v.Mutators = append(v.Mutators, mus...)
		return ProcessWith(ctx, v)
	}
	return ProcessWith(ctx, &Config{
		Target:   i,
		Mutators: mus,
	})
}

// MustProcess is a helper that calls [Process] and panics if an error is
// encountered. Unlike [Process], the input value is returned, making it ideal
// for anonymous initializations:
//
//	var env = envconfig.MustProcess(context.Background(), &struct{
//	  Field string `env:"FIELD,required"`
//	})
//
// This is not recommend for production services, but it can be useful for quick
// CLIs and scripts that want to take advantage of envconfig's environment
// parsing at the expense of testability and graceful error handling.
func MustProcess[T any](ctx context.Context, i T, mus ...Mutator) T {
	if err := Process(ctx, i, mus...); err != nil {
		panic(err)
	}
	return i
}

// ProcessWith executes the decoding process using the provided [Config].
func ProcessWith(ctx context.Context, c *Config) error {
	if c == nil {
		c = new(Config)
	}

	if c.Lookuper == nil {
		c.Lookuper = OsLookuper()
	}

	// Deep copy the slice and remove any nil mutators.
	var mus []Mutator
	for _, m := range c.Mutators {
		if m != nil {
			mus = append(mus, m)
		}
	}
	c.Mutators = mus

	return processWith(ctx, c)
}

// processWith is a helper that retains configuration from the parent structs.
func processWith(ctx context.Context, c *Config) error {
	i := c.Target

	l := c.Lookuper
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

	structDelimiter := c.DefaultDelimiter
	if structDelimiter == "" {
		structDelimiter = ","
	}

	structNoInit := c.DefaultNoInit

	structSeparator := c.DefaultSeparator
	if structSeparator == "" {
		structSeparator = ":"
	}

	structOverwrite := c.DefaultOverwrite
	structDecodeUnset := c.DefaultDecodeUnset
	structRequired := c.DefaultRequired

	mutators := c.Mutators

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
		if opts.NoInit &&
			ef.Kind() != reflect.Ptr &&
			ef.Kind() != reflect.Slice &&
			ef.Kind() != reflect.Map &&
			ef.Kind() != reflect.UnsafePointer {
			return fmt.Errorf("%s: %w", tf.Name, ErrNoInitNotPtr)
		}

		// Compute defaults from local tags.
		delimiter := structDelimiter
		if v := opts.Delimiter; v != "" {
			delimiter = v
		}
		separator := structSeparator
		if v := opts.Separator; v != "" {
			separator = v
		}

		noInit := structNoInit || opts.NoInit
		overwrite := structOverwrite || opts.Overwrite
		decodeUnset := structDecodeUnset || opts.DecodeUnset
		required := structRequired || opts.Required

		isNilStructPtr := false
		setNilStruct := func(v reflect.Value) {
			origin := e.Field(i)
			if isNilStructPtr {
				empty := reflect.New(origin.Type().Elem()).Interface()

				// If a struct (after traversal) equals to the empty value, it means
				// nothing was changed in any sub-fields. With the noinit opt, we skip
				// setting the empty value to the original struct pointer (keep it nil).
				if !reflect.DeepEqual(v.Interface(), empty) || !noInit {
					origin.Set(v)
				}
			}
		}

		// Initialize pointer structs.
		pointerWasSet := false
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
				pointerWasSet = true
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
			val, found, usedDefault, err := lookup(key, required, opts.Default, l)
			if err != nil && !errors.Is(err, ErrMissingKey) {
				return fmt.Errorf("%s: %w", tf.Name, err)
			}

			if found || usedDefault || decodeUnset {
				if ok, err := processAsDecoder(ctx, val, ef); ok {
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

			if err := processWith(ctx, &Config{
				Target:           ef.Interface(),
				Lookuper:         plu,
				DefaultDelimiter: delimiter,
				DefaultSeparator: separator,
				DefaultNoInit:    noInit,
				DefaultOverwrite: overwrite,
				DefaultRequired:  required,
				Mutators:         mutators,
			}); err != nil {
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
		if (pointerWasSet || !ef.IsZero()) && !overwrite {
			continue
		}

		val, found, usedDefault, err := lookup(key, required, opts.Default, l)
		if err != nil {
			return fmt.Errorf("%s: %w", tf.Name, err)
		}

		// If the field already has a non-zero value and there was no value directly
		// specified, do not overwrite the existing field. We only want to overwrite
		// when the envvar was provided directly.
		if (pointerWasSet || !ef.IsZero()) && !found {
			continue
		}

		// Apply any mutators. Mutators are applied after the lookup, but before any
		// type conversions. They always resolve to a string (or error), so we don't
		// call mutators when the environment variable was not set.
		if found || usedDefault {
			originalKey := key
			resolvedKey := originalKey
			if keyer, ok := l.(keyedLookuper); ok {
				resolvedKey = keyer.Key(resolvedKey)
			}
			originalValue := val
			stop := false

			for _, mu := range mutators {
				val, stop, err = mu.EnvMutate(ctx, originalKey, resolvedKey, originalValue, val)
				if err != nil {
					return fmt.Errorf("%s: %w", tf.Name, err)
				}
				if stop {
					break
				}
			}
		}

		// Set value.
		if err := processField(ctx, val, ef, delimiter, separator, noInit); err != nil {
			return fmt.Errorf("%s: %w", tf.Name, err)
		}
	}

	return nil
}

// SplitString splits the given string on the provided rune, unless the rune is
// escaped by the escape character.
func splitString(s, on, esc string) []string {
	a := strings.Split(s, on)

	for i := len(a) - 2; i >= 0; i-- {
		if strings.HasSuffix(a[i], esc) {
			a[i] = a[i][:len(a[i])-len(esc)] + on + a[i+1]
			a = append(a[:i+1], a[i+2:]...)
		}
	}
	return a
}

// keyAndOpts parses the given tag value (e.g. env:"foo,required") and
// returns the key name and options as a list.
func keyAndOpts(tag string) (string, *options, error) {
	parts := splitString(tag, ",", "\\")
	key, tagOpts := strings.TrimSpace(parts[0]), parts[1:]

	if key != "" && !validateEnvName(key) {
		return "", nil, fmt.Errorf("%q: %w ", key, ErrInvalidEnvvarName)
	}

	var opts options

LOOP:
	for i, o := range tagOpts {
		o = strings.TrimLeftFunc(o, unicode.IsSpace)
		search := strings.ToLower(o)

		switch {
		case search == optDecodeUnset:
			opts.DecodeUnset = true
		case search == optOverwrite:
			opts.Overwrite = true
		case search == optRequired:
			opts.Required = true
		case search == optNoInit:
			opts.NoInit = true
		case strings.HasPrefix(search, optPrefix):
			opts.Prefix = strings.TrimPrefix(o, optPrefix)
		case strings.HasPrefix(search, optDelimiter):
			opts.Delimiter = strings.TrimPrefix(o, optDelimiter)
		case strings.HasPrefix(search, optSeparator):
			opts.Separator = strings.TrimPrefix(o, optSeparator)
		case strings.HasPrefix(search, optDefault):
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
func lookup(key string, required bool, defaultValue string, l Lookuper) (string, bool, bool, error) {
	if key == "" {
		// The struct has something like `env:",required"`, which is likely a
		// mistake. We could try to infer the envvar from the field name, but that
		// feels too magical.
		return "", false, false, ErrMissingKey
	}

	if required && defaultValue != "" {
		// Having a default value on a required value doesn't make sense.
		return "", false, false, ErrRequiredAndDefault
	}

	// Lookup value.
	val, found := l.Lookup(key)
	if !found {
		if required {
			if keyer, ok := l.(keyedLookuper); ok {
				key = keyer.Key(key)
			}

			return "", false, false, fmt.Errorf("%w: %s", ErrMissingRequired, key)
		}

		if defaultValue != "" {
			// Handle escaped "$" by replacing the value with a character that is
			// invalid to have in an environment variable. A more perfect solution
			// would be to re-implement os.Expand to handle this case, but that's been
			// proposed and rejected in the stdlib. Additionally, the function is
			// dependent on other private functions in the [os] package, so
			// duplicating it is toilsome.
			//
			// While admittidly a hack, replacing the escaped values with invalid
			// characters (and then replacing later), is a reasonable solution.
			defaultValue = strings.ReplaceAll(defaultValue, "\\\\", "\u0000")
			defaultValue = strings.ReplaceAll(defaultValue, "\\$", "\u0008")

			// Expand the default value. This allows for a default value that maps to
			// a different environment variable.
			val = os.Expand(defaultValue, func(i string) string {
				lookuper := l
				if v, ok := lookuper.(unwrappableLookuper); ok {
					lookuper = v.Unwrap()
				}

				s, ok := lookuper.Lookup(i)
				if ok {
					return s
				}
				return ""
			})

			val = strings.ReplaceAll(val, "\u0000", "\\")
			val = strings.ReplaceAll(val, "\u0008", "$")

			return val, false, true, nil
		}
	}

	return val, found, false, nil
}

// processAsDecoder processes the given value as a decoder or custom
// unmarshaller.
func processAsDecoder(ctx context.Context, v string, ef reflect.Value) (bool, error) {
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
		if dec, ok := iface.(DecoderCtx); ok {
			imp = true
			err = dec.EnvDecode(ctx, v)
			return imp, err
		}

		// Check legacy decoder implementation
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

func processField(ctx context.Context, v string, ef reflect.Value, delimiter, separator string, noInit bool) error {
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
	if ok, err := processAsDecoder(ctx, v, ef); ok {
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
			if err := processField(ctx, mKey, k, delimiter, separator, noInit); err != nil {
				return fmt.Errorf("%s: %w", mKey, err)
			}

			v := reflect.New(tf.Elem()).Elem()
			if err := processField(ctx, mVal, v, delimiter, separator, noInit); err != nil {
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
				if err := processField(ctx, val, s.Index(i), delimiter, separator, noInit); err != nil {
					return fmt.Errorf("%s: %w", val, err)
				}
			}
			ef.Set(s)
		}
	}

	return nil
}

// validateEnvName validates the given string conforms to being a valid
// environment variable.
//
// Per IEEE Std 1003.1-2001 environment variables consist solely of uppercase
// letters, digits, and _, and do not begin with a digit.
func validateEnvName(s string) bool {
	if s == "" {
		return false
	}

	for i, r := range s {
		if (i == 0 && !isLetter(r) && r != '_') || (!isLetter(r) && !isNumber(r) && r != '_') {
			return false
		}
	}

	return true
}

// isLetter returns true if the given rune is a letter between a-z,A-Z. This is
// different than unicode.IsLetter which includes all L character cases.
func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isNumber returns true if the given run is a number between 0-9. This is
// different than unicode.IsNumber in that it only allows 0-9.
func isNumber(r rune) bool {
	return r >= '0' && r <= '9'
}
