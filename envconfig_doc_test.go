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

	"github.com/sethvargo/go-envconfig"
)

var ctx = context.Background()

func Example_inherited() {
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

func Example_globalConfigurations() {
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
