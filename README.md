# Envconfig

[![GoDoc](https://img.shields.io/badge/go-documentation-blue.svg?style=flat-square)][godoc]

Envconfig populates struct field values based on environment variables or
arbitrary lookup functions. It supports pre-setting mutations, which is useful
for things like converting values to uppercase, trimming whitespace, or looking
up secrets.

## Usage

Define a struct with fields using the `env` tag:

```go
type MyConfig struct {
  Port     int    `env:"PORT"`
  Username string `env:"USERNAME"`
}
```

Set some environment variables:

```sh
export PORT=5555
export USERNAME=yoyo
```

Process it using envconfig:

```go
package main

import (
  "context"
  "log"

  "github.com/sethvargo/go-envconfig"
)

func main() {
  ctx := context.Background()

  var c MyConfig
  if err := envconfig.Process(ctx, &c); err != nil {
    log.Fatal(err)
  }

  // c.Port = 5555
  // c.Username = "yoyo"
}
```

You can also use nested structs, just remember that any fields you want to
process must be public:

```go
type MyConfig struct {
  Database *DatabaseConfig
}

type DatabaseConfig struct {
  Port     int    `env:"PORT"`
  Username string `env:"USERNAME"`
}
```

## Configuration

Use the `env` struct tag to define configuration.

### Required

If a field is required, processing will error if the environment variable is
unset.

```go
type MyStruct struct {
  Port int `env:"PORT,required"`
}
```

It is invalid to have a field as both `required` and `default`.

### Default

If an environment variable is not set, the field will be set to the default
value. Note that the environment variable must not be set (e.g. `unset PORT`).
If the environment variable is the empty string, that counts as a "value" and
the default will not be used.

```go
type MyStruct struct {
  Port int `env:"PORT,default=5555"`
}
```

You can also set the default value to another field or value from the
environment, for example:

```go
type MyStruct struct {
  DefaultPort int `env:"DEFAULT_PORT,default=5555"`
  Port        int `env:"OVERRIDE_PORT,default=$DEFAULT_PORT"`
}
```

The value for `Port` will default to the value of `DEFAULT_PORT`.

It is invalid to have a field as both `required` and `default`.

### Prefix

For shared, embedded structs, you can define a prefix to use when processing
struct values for that embed.

```go
type SharedConfig struct {
  Port int `env:"PORT,default=5555"`
}

type Server1 struct {
  // This processes Port from $FOO_PORT.
  *SharedConfig `env:",prefix=FOO_"`
}

type Server2 struct {
  // This processes Port from $BAR_PORT.
  *SharedConfig `env:",prefix=BAR_"`
}
```

It is invalid to specify a prefix on non-struct fields.

### Overwrite

If overwrite is set, the value will be overwritten if there is an environment
variable match regardless if the value is non-zero.

```go
type MyStruct struct {
  Port int `env:"PORT,overwrite"`
}
```

The rules for overwrite + default are:

-   If the struct field has the zero value and a default is set:

    -   If no environment variable is specified, the struct field will be
        populated with the default value.

    -   If an environment variable is specified, the struct field will be
        populate with the environment variable value.

-   If the struct field has a non-zero value and a default is set:

    -   If no environment variable is specified, the struct field's existing
        value will be used (the default is ignored).

    -   If an environment variable is specified, the struct field's existing
        value will be overwritten with the environment variable value.


## Complex Types

**Note:** Complex types are only decoded or unmarshalled when the environment
variable is defined or a default is specified. The decoding/unmarshalling
functions are _not_ invoked when a value is not defined.

### Durations

In the environment, `time.Duration` values are specified as a parsable Go
duration:

```go
type MyStruct struct {
  MyVar time.Duration `env:"MYVAR"`
}
```

```bash
export MYVAR="10m" # 10 * time.Minute
```

### TextUnmarshaler / BinaryUnmarshaler

Types that implement `TextUnmarshaler` or `BinaryUnmarshaler` are processed as such.

### json.Unmarshaler

Types that implement `json.Unmarshaler` are processed as such.

### gob.Decoder

Types that implement `gob.Decoder` are processed as such.


### Slices

Slices are specified as comma-separated values:

```go
type MyStruct struct {
  MyVar []string `env:"MYVAR"`
}
```

```bash
export MYVAR="a,b,c,d" # []string{"a", "b", "c", "d"}
```

Define a custom delimiter with `delimiter`:

```go
type MyStruct struct {
  MyVar []string `env:"MYVAR,delimiter=;"`
```

```bash
export MYVAR="a;b;c;d" # []string{"a", "b", "c", "d"}
```

Note that byte slices are special cased and interpreted as strings from the
environment.

### Maps

Maps are specified as comma-separated key:value pairs:

```go
type MyStruct struct {
  MyVar map[string]string `env:"MYVAR"`
}
```

```bash
export MYVAR="a:b,c:d" # map[string]string{"a":"b", "c":"d"}
```

Define a custom delimiter with `delimiter`:

```go
type MyStruct struct {
  MyVar map[string]string `env:"MYVAR,delimiter=;"`
```

```bash
export MYVAR="a:b;c:d" # map[string]string{"a":"b", "c":"d"}
```

Define a separator with `separator`:

```go
type MyStruct struct {
  MyVar map[string]string `env:"MYVAR,separator=|"`
}
```

```bash
export MYVAR="a|b,c|d" # map[string]string{"a":"b", "c":"d"}
```


### Structs

Envconfig walks the entire struct, including nested structs, so deeply-nested
fields are also supported.

If a nested struct is a pointer type, it will automatically be instantianted to
the non-nil value. To change this behavior, see
[Initialization](#Initialization).


### Custom

You can also [define your own decoder](#Extension).


## Prefixing

You can define a custom prefix using the `PrefixLookuper`. This will lookup
values in the environment by prefixing the keys with the provided value:

```go
type MyStruct struct {
  MyVar string `env:"MYVAR"`
}
```

```go
// Process variables, but look for the "APP_" prefix.
if err := envconfig.ProcessWith(ctx, &c, &envconfig.Config{
  Lookuper: envconfig.PrefixLookuper("APP_", envconfig.OsLookuper()),
}); err != nil {
  panic(err)
}
```

```bash
export APP_MYVAR="foo"
```

## Initialization

By default, all pointers, slices, and maps are initialized (allocated) so they
are not `nil`. To disable this behavior, use the tag the field as `noinit`:

```go
type MyStruct struct {
  // Without `noinit`, DeleteUser would be initialized to the default boolean
  // value. With `noinit`, if the environment variable is not given, the value
  // is kept as uninitialized (nil).
  DeleteUser *bool `env:"DELETE_USER, noinit"`
}
```

This also applies to nested fields in a struct:

```go
type ParentConfig struct {
  // Without `noinit` tag, `Child` would be set to `&ChildConfig{}` whether
  // or not `FIELD` is set in the env var.
  // With `noinit`, `Child` would stay nil if `FIELD` is not set in the env var.
  Child *ChildConfig `env:",noinit"`
}

type ChildConfig struct {
  Field string `env:"FIELD"`
}
```

The `noinit` tag is only applicable for pointer, slice, and map fields. Putting
the tag on a different type will return an error.


## Extension

### Decoders

All built-in types are supported except `Func` and `Chan`. If you need to define
a custom decoder, implement the `Decoder` interface:

```go
type MyStruct struct {
  field string
}

func (v *MyStruct) EnvDecode(val string) error {
  v.field = fmt.Sprintf("PREFIX-%s", val)
  return nil
}
```

### Mutators

If you need to modify environment variable values before processing, you can
specify a custom `Mutator`:

```go
type Config struct {
  Password `env:"PASSWORD"`
}

func resolveSecretFunc(ctx context.Context, originalKey, resolvedKey, originalValue, resolvedValue string) (newValue string, stop bool, err error) {
  if strings.HasPrefix(value, "secret://") {
    v, err := secretmanager.Resolve(ctx, value) // example
    if err != nil {
      return resolvedValue, true, fmt.Errorf("failed to access secret: %w", err)
    }
    return v, false, nil
  }
  return resolvedValue, false, nil
}

var config Config
envconfig.ProcessWith(ctx, &config, &envconfig.Config{
  Lookuper:  envconfig.OsLookuper(),
  Mutators: []envconfig.Mutator{resolveSecretFunc},
})
```

Mutators are like middleware, and they have access to the initial and current
state of the stack. Mutators only run when a value has been provided in the
environment. They execute _before_ any complex type processing, so all inputs
and outputs are strings. To create a mutator, implement `EnvMutate` or use
`MutatorFunc`:

```go
type MyMutator struct {}

func (m *MyMutator) EnvMutate(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (newValue string, stop bool, err error) {
  // ...
}

//
// OR
//

envconfig.MutatorFunc(func(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (newValue string, stop bool, err error) {
  // ...
})
```

The parameters (in order) are:

-   `context` is the context provided to `Process`.

-   `originalKey` is the unmodified environment variable name as it was defined
    on the struct.

-   `resolvedKey` is the fully-resolved environment variable name, which may
    include prefixes or modifications from processing. When there are no
    modifications, this will be equivalent to `originalKey`.

-   `originalValue` is the unmodified environment variable's value before any
    mutations were run.

-   `currentValue` is the currently-resolved value, which may have been modified
    by previous mutators and may be modified by subsequent mutators in the
    stack.

The function returns (in order):

-   The new value to use in both future mutations and final processing.

-   A boolean which indicates whether future mutations in the stack should be
    applied.

-   Any errors that occurred.

> [!TIP]
>
> Users coming from the v0 series can wrap their mutator functions with
> `LegacyMutatorFunc` for an easier transition to this new syntax.

Consider the following example to illustrate the difference between
`originalKey` and `resolvedKey`:

```go
type Config struct {
  Password `env:"PASSWORD"`
}

var config Config
mutators := []envconfig.Mutators{mutatorFunc1, mutatorFunc2, mutatorFunc3}
envconfig.ProcessWith(ctx, &config, &envconfig.Config{
  Lookuper: envconfig.PrefixLookuper("REDIS_", envconfig.MapLookuper(map[string]string{
    "PASSWORD": "original",
  })),
  Mutators: mutators,
})

func mutatorFunc1(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (string, bool, error) {
  // originalKey is "PASSWORD"
  // resolvedKey is "REDIS_PASSWORD"
  // originalValue is "original"
  // currentValue is "original"
  return currentValue+"-modified", false, nil
}

func mutatorFunc2(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (string, bool, error) {
  // originalKey is "PASSWORD"
  // resolvedKey is "REDIS_PASSWORD"
  // originalValue is "original"
  // currentValue is "original-modified"
  return currentValue, true, nil
}

func mutatorFunc3(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (string, bool, error) {
  // This mutator will never run because mutatorFunc2 stopped the chain.
  return "...", false, nil
}
```


## Advanced Processing

See the [godoc][] for examples.


## Testing

Relying on the environment in tests can be troublesome because environment
variables are global, which makes it difficult to parallelize the tests.
Envconfig supports extracting data from anything that returns a value:

```go
lookuper := envconfig.MapLookuper(map[string]string{
  "FOO": "bar",
  "ZIP": "zap",
})

var config Config
envconfig.ProcessWith(ctx, &config, &envconfig.Config{
  Lookuper: lookuper,
})
```

Now you can parallelize all your tests by providing a map for the lookup
function. In fact, that's how the tests in this repo work, so check there for an
example.

You can also combine multiple lookupers with `MultiLookuper`. See the GoDoc for
more information and examples.


[godoc]: https://pkg.go.dev/mod/github.com/sethvargo/go-envconfig
