# Envconfig

[![GoDoc](https://img.shields.io/badge/GoDoc-reference-007d9c?style=flat-square)](https://pkg.go.dev/github.com/sethvargo/go-envconfig)

Envconfig populates struct fields based on environment variable
values.

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

  "github.com/sethvargo/envconfig/pkg/go-envconfig"
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

## Complex Types

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


### Slices

In the environment, slices are specified as comma-separated values:

```go
type MyStruct struct {
  MyVar []string `env:"MYVAR"`
}
```

```bash
export MYVAR="a,b,c,d" # []string{"a", "b", "c", "d"}
```

Note that byte slices are special cased and interpreted as strings from the
environment.

### Maps

In the environment, maps are specified as comma-separated key:value pairs:

```go
type MyStruct struct {
  MyVar map[string]string `env:"MYVAR"`
}
```

```bash
export MYVAR="a:b,c:d" # map[string]string{"a":"b", "c":"d"}
```

### Structs

Envconfig walks the entire struct, so deeply-nested fields are also supported. You can also define your own decoder (see below).


## Extension

All built-in types are supported except Func and Chan. If you need to define a
custom decoder, implement `Decoder`:

```go
type MyStruct struct {
  field string
}

func (v *MyStruct) EnvDecode(val string) error {
  v.field = fmt.Sprintf("PREFIX-%s", val)
  return nil
}
```

If you need to modify environment variable values before processing, you can
specify a custom `Mutator`:

```go
type Config struct {
  Password `env:"PASSWORD"`
}

func resolveSecretFunc(ctx context.Context, key, value string) (string, error) {
  if strings.HasPrefix(key, "secret://") {
    return secretmanager.Resolve(ctx, value) // example
  }
  return value, nil
}

var config Config
ProcessWith(&config, OsLookuper(), resolveSecretFunc)
```

## Inspiration

This library is conceptually similar to [kelseyhightower/envconfig](https://github.com/kelseyhightower/envconfig), with the following
major behavioral differences:

-   Adds support for specifying a custom lookup function (such as a map), which
    is useful for testing.

-   Only populates fields if they contain zero or nil values. This means you can
    pre-initialize a struct and any pre-populated fields will not be overwritten
    during processing.

-   Support for interpolation. The default value for a field can be the value of
    another field.

-   Support for arbitrary mutators that change/resolve data before type
    conversion.
