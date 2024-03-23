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

package envconfig_test

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/sethvargo/go-envconfig"
)

var ctx = context.Background()

var secretmanager = &testSecretManager{}

type testSecretManager struct{}

func (s *testSecretManager) Resolve(ctx context.Context, value string) (string, error) {
	return "", nil
}

func Example_basic() {
	// This example demonstrates the basic usage for envconfig.

	type MyStruct struct {
		Port     int    `env:"PORT"`
		Username string `env:"USERNAME"`
	}

	// Set some environment variables in the process:
	//
	//     export PORT=5555
	//     export USERNAME=yoyo
	//

	var s MyStruct
	if err := envconfig.Process(ctx, &s); err != nil {
		panic(err) // TODO: handle error
	}

	// c.Port = 5555
	// c.Username = "yoyo"
}

func Example_mapLookuper() {
	// This example demonstrates using a [MapLookuper] to source environment
	// variables from a map instead of the environment. The map will always be of
	// type string=string, because environment variables are always string types.

	type MyStruct struct {
		Port     int    `env:"PORT"`
		Username string `env:"USERNAME"`
	}

	var s MyStruct
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{
		Target: &s,
		Lookuper: envconfig.MapLookuper(map[string]string{
			"PORT":     "5555",
			"USERNAME": "yoyo",
		}),
	}); err != nil {
		panic(err) // TODO: handle error
	}

	fmt.Printf("port: %d\n", s.Port)
	fmt.Printf("username: %q\n", s.Username)

	// Output:
	// port: 5555
	// username: "yoyo"
}

func Example_required() {
	// This example demonstrates how to set fields as required. Required fields
	// will error if unset.

	type MyStruct struct {
		Port int `env:"PORT, required"`
	}

	var s MyStruct
	if err := envconfig.Process(ctx, &s); err != nil {
		fmt.Printf("error: %s\n", err)
	}

	// Output:
	// error: Port: missing required value: PORT
}

func Example_defaults() {
	// This example demonstrates how to set default values for fields. Fields will
	// use their default value if no value is provided for that key in the
	// environment.

	type MyStruct struct {
		Port     int    `env:"PORT, default=8080"`
		Username string `env:"USERNAME, default=$OTHER_ENV"`
	}

	var s MyStruct
	if err := envconfig.Process(ctx, &s); err != nil {
		panic(err) // TODO: handle error
	}

	fmt.Printf("port: %d\n", s.Port)

	// Output:
	// port: 8080
}

func Example_prefix() {
	// This example demonstrates using prefixes to share structures.

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

	var s ServerConfig
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{
		Target: &s,
		Lookuper: envconfig.MapLookuper(map[string]string{
			"CACHE_REDIS_HOST":      "https://cache.host.internal",
			"CACHE_REDIS_USER":      "cacher",
			"RATE_LIMIT_REDIS_HOST": "https://limiter.host.internal",
			"RATE_LIMIT_REDIS_USER": "limiter",
		}),
	}); err != nil {
		panic(err) // TODO: handle error
	}

	fmt.Printf("cache redis host: %s\n", s.CacheConfig.Host)
	fmt.Printf("cache redis user: %s\n", s.CacheConfig.User)
	fmt.Printf("rate limit redis host: %s\n", s.RateLimitConfig.Host)
	fmt.Printf("rate limit redis user: %s\n", s.RateLimitConfig.User)

	// Output:
	// cache redis host: https://cache.host.internal
	// cache redis user: cacher
	// rate limit redis host: https://limiter.host.internal
	// rate limit redis user: limiter
}

func Example_overwrite() {
	// This example demonstrates how to tell envconfig to overwrite existing
	// struct values.

	type MyStruct struct {
		Port int `env:"PORT, overwrite"`
	}

	s := &MyStruct{
		Port: 1234,
	}
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{
		Target: s,
		Lookuper: envconfig.MapLookuper(map[string]string{
			"PORT": "8080",
		}),
	}); err != nil {
		panic(err) // TODO: handle error
	}

	fmt.Printf("port: %d\n", s.Port)

	// Output:
	// port: 8080
}

func Example_prefixLookuper() {
	// This example demonstrates using a [PrefixLookuper] to programatically alter
	// environment variable keys to include the given prefix.

	type MyStruct struct {
		Port int `env:"PORT"`
	}

	var s MyStruct
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{
		Target: &s,
		Lookuper: envconfig.PrefixLookuper("APP_", envconfig.MapLookuper(map[string]string{
			"APP_PORT": "1234",
		})),
	}); err != nil {
		panic(err)
	}

	fmt.Printf("port: %d\n", s.Port)

	// Output:
	// port: 1234
}

func Example_noinit() {
	// This example demonstrates setting the "noinit" tag to bypass
	// initialization.

	type MyStruct struct {
		SecureA *bool `env:"SECURE_A"`
		SecureB *bool `env:"SECURE_B, noinit"`
	}

	var s MyStruct
	if err := envconfig.Process(ctx, &s); err != nil {
		panic(err)
	}

	printVal := func(v *bool) string {
		if v != nil {
			return strconv.FormatBool(*v)
		}
		return "<nil>"
	}

	fmt.Printf("secureA: %s\n", printVal(s.SecureA))
	fmt.Printf("secureB: %s\n", printVal(s.SecureB))

	// Output:
	// secureA: false
	// secureB: <nil>
}

func Example_decodeunset() {
	// This example demonstrates forcing envconfig to run decoders, even on unset
	// environment variables.

	type MyStruct struct {
		UrlA *url.URL `env:"URL_A"`
		UrlB *url.URL `env:"URL_B, decodeunset"`
	}

	var s MyStruct
	if err := envconfig.Process(ctx, &s); err != nil {
		panic(err)
	}

	fmt.Printf("urlA: %s\n", s.UrlA)
	fmt.Printf("urlB: %s\n", s.UrlB)

	// Output:
	// urlA: //@
	// urlB:
}

func Example_mutatorFunc() {
	// This example demonstrates authoring mutator functions to modify environment
	// variable values before processing.

	type MyStruct struct {
		Password string `env:"PASSWORD"`
	}

	resolveSecretFunc := envconfig.MutatorFunc(func(ctx context.Context, originalKey, resolvedKey, originalValue, resolvedValue string) (newValue string, stop bool, err error) {
		if strings.HasPrefix(resolvedValue, "secret://") {
			v, err := secretmanager.Resolve(ctx, resolvedValue)
			if err != nil {
				return resolvedValue, true, fmt.Errorf("failed to access secret: %w", err)
			}
			return v, false, nil
		}
		return resolvedValue, false, nil
	})

	var s MyStruct
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{
		Target:   &s,
		Lookuper: envconfig.OsLookuper(),
		Mutators: []envconfig.Mutator{resolveSecretFunc},
	}); err != nil {
		panic(err) // TODO: handle error
	}
}

func Example_inheritedConfiguration() {
	// This example demonstrates how struct-level configuration options are
	// propagated to child fields and structs.

	type Credentials struct {
		Username string `env:"USERNAME"`
		Password string `env:"PASSWORD"`
	}

	type Metadata struct {
		Headers map[string]string  `env:"HEADERS"`
		Footers []string           `env:"FOOTERS"`
		Margins map[string]float64 `env:"MARGINS, delimiter=\\,, separator=:"`
	}

	type ConnectionInfo struct {
		Address string `env:"ADDRESS"`

		// All child fields will be required.
		Credentials *Credentials `env:",required"`

		// All child fields will use a ";" delimiter and "@" separator, unless
		// locally overridden.
		Metadata *Metadata `env:",delimiter=;, separator=@"`
	}

	var conn ConnectionInfo
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{
		Target: &conn,
		Lookuper: envconfig.MapLookuper(map[string]string{
			"ADDRESS":  "127.0.0.1",
			"USERNAME": "user",
			"PASSWORD": "pass",
			"HEADERS":  "header1@value1;header2@value2",
			"FOOTERS":  "footer1; footer2",
			"MARGINS":  "top:0.5, bottom:1.5",
		}),
	}); err != nil {
		panic(err)
	}

	fmt.Printf("address: %q\n", conn.Address)
	fmt.Printf("username: %q\n", conn.Credentials.Username)
	fmt.Printf("password: %q\n", conn.Credentials.Password)
	fmt.Printf("headers: %v\n", conn.Metadata.Headers)
	fmt.Printf("footers: %q\n", conn.Metadata.Footers)
	fmt.Printf("margins: %v\n", conn.Metadata.Margins)

	// Output:
	// address: "127.0.0.1"
	// username: "user"
	// password: "pass"
	// headers: map[header1:value1 header2:value2]
	// footers: ["footer1" "footer2"]
	// margins: map[bottom:1.5 top:0.5]
}

func Example_customConfiguration() {
	// This example demonstrates how to set global configuration options that
	// apply to all decoding (unless overridden at the field level).

	type HTTPConfig struct {
		AllowedHeaders  map[string]string `env:"ALLOWED_HEADERS"`
		RejectedHeaders map[string]string `env:"REJECTED_HEADERS, delimiter=|"`
	}

	var httpConfig HTTPConfig
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{
		Target: &httpConfig,

		// All fields will use a ";" delimiter and "@" separator, unless locally
		// overridden.
		DefaultDelimiter: ";",
		DefaultSeparator: "@",

		Lookuper: envconfig.MapLookuper(map[string]string{
			"ALLOWED_HEADERS":  "header1@value1;header2@value2",
			"REJECTED_HEADERS": "header3@value3|header4@value4",
		}),
	}); err != nil {
		panic(err)
	}

	fmt.Printf("allowed: %v\n", httpConfig.AllowedHeaders)
	fmt.Printf("rejected: %v\n", httpConfig.RejectedHeaders)

	// Output:
	// allowed: map[header1:value1 header2:value2]
	// rejected: map[header3:value3 header4:value4]
}
