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
  Port     string `env:"PORT"`
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
  Port     string `env:"PORT"`
  Username string `env:"USERNAME"`
}
```

## Configuration

Use the `env` struct tag to define configuration. See the [godoc][] for usage
examples.

-   `required` - marks a field as required. If a field is required, decoding
    will error if the environment variable is unset.

    ```go
    type MyStruct struct {
      Port string `env:"PORT, required"`
    }
    ```

-   `default` - sets the default value for the environment variable is not set.
    The environment variable must not be set (e.g. `unset PORT`). If the
    environment variable is the empty string, envconfig considers that a "value"
    and the default will **not** be used.

    You can also set the default value to the value from another field or a
    value from a different environment variable.

    ```go
    type MyStruct struct {
      Port string `env:"PORT, default=5555"`
      User string `env:"USER, default=$CURRENT_USER"`
    }
    ```

    As a special case where the default value should contain a literal `$`,
    escape it with a backslash. Unfortunately this requires a double backslash
    in the struct tag:

    ```go
    type MyStruct struct {
      Amount string `env:"AMOUNT, default=\\$5.00"` // Default: $5.00
    }
    ```

    To have a literal backslash followed by a `$`, escape the backslash:

    ```go
    type MyStruct struct {
      Filepath string `env:"FILEPATH, default=C:\\Personal\\\\$name"` // Default: C:\Personal\$name
    }
    ```

-   `prefix` - sets the prefix to use for looking up environment variable keys
    on child structs and fields. This is useful for shared configurations:

    ```go
    type RedisConfig struct {
      Host string `env:"REDIS_HOST"`
      User string `env:"REDIS_USER"`
    }

    type ServerConfig struct {
      // CacheConfig will process values from $CACHE_REDIS_HOST and
      // $CACHE_REDIS_USER respectively.
      CacheConfig *RedisConfig `env:", prefix=CACHE_"`

      // RateLimitConfig will process values from $RATE_LIMIT_REDIS_HOST and
      // $RATE_LIMIT_REDIS_USER respectively.
      RateLimitConfig *RedisConfig `env:", prefix=RATE_LIMIT_"`
    }
    ```

-   `overwrite` - force overwriting existing non-zero struct values if the
    environment variable was provided.

    ```go
    type MyStruct struct {
      Port string `env:"PORT, overwrite"`
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

-   `delimiter` - choose a custom character to denote individual slice and map
    entries. The default value is the comma (`,`).

    ```go
    type MyStruct struct {
      MyVar []string `env:"MYVAR, delimiter=;"`
    ```

    ```bash
    export MYVAR="a;b;c;d" # []string{"a", "b", "c", "d"}
    ```

-   `separator` - choose a custom character to denote the separation between
    keys and values in map entries. The default value is the colon (`:`) Define
    a separator with `separator`:

    ```go
    type MyStruct struct {
      MyVar map[string]string `env:"MYVAR, separator=|"`
    }
    ```

    ```bash
    export MYVAR="a|b,c|d" # map[string]string{"a":"b", "c":"d"}
    ```

-   `noinit` - do not initialize struct fields unless environment variables were
    provided. The default behavior is to deeply initialize all fields to their
    default (zero) value.

    ```go
    type MyStruct struct {
      MyVar *url.URL `env:"MYVAR, noinit"`
    }
    ```

-   `decodeunset` - force envconfig to run decoders even on unset environment
    variable values. The default behavior is to skip running decoders on unset
    environment variable values.

    ```go
    type MyStruct struct {
      MyVar *url.URL `env:"MYVAR, decodeunset"`
    }
    ```


## Decoding

> [!NOTE]
>
> Complex types are only decoded or unmarshalled when the environment variable
> is defined or a default value is specified.


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

Types that implement `TextUnmarshaler` or `BinaryUnmarshaler` are processed as
such.


### json.Unmarshaler

Types that implement `json.Unmarshaler` are processed as such.


### gob.Decoder

Types that implement `gob.Decoder` are processed as such.


### Slices

Slices are specified as comma-separated values.

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

Maps are specified as comma-separated key:value pairs:

```go
type MyStruct struct {
  MyVar map[string]string `env:"MYVAR"`
}
```

```bash
export MYVAR="a:b,c:d" # map[string]string{"a":"b", "c":"d"}
```


### Structs

Envconfig walks the entire struct, including nested structs, so deeply-nested
fields are also supported.

If a nested struct is a pointer type, it will automatically be instantianted to
the non-nil value. To change this behavior, see
[Initialization](#Initialization).


### Custom Decoders

You can also define your own decoders.

```go
type MyCustomType struct {
  value string
}

func (t *MyCustomType) EnvDecode(ctx context.Context, val string) error {
  resolved := someComplexFunction(val)
  t.value = resolved
  return nil
}
```

See the [godoc][godoc] for more information.


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
envconfig.ProcessWith(ctx, &envconfig.Config{
  Target:   &config,
  Lookuper: lookuper,
})
```

Now you can parallelize all your tests by providing a map for the lookup
function. In fact, that's how the tests in this repo work, so check there for an
example.

You can also combine multiple lookupers with `MultiLookuper`. See the GoDoc for
more information and examples.


[godoc]: https://pkg.go.dev/mod/github.com/sethvargo/go-envconfig
