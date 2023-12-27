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
	"encoding/json"
	"fmt"

	"github.com/sethvargo/go-envconfig"
)

type CustomStruct struct {
	Port string `json:"port"`
	User string `json:"user"`
	Max  int    `json:"max"`
}

func (s *CustomStruct) EnvDecode(val string) error {
	return json.Unmarshal([]byte(val), s)
}

func Example_decoder() {
	// This example demonstrates defining a custom decoder function.

	type MyStruct struct {
		Config CustomStruct `env:"CONFIG"`
	}

	var s MyStruct
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{
		Target: &s,
		Lookuper: envconfig.MapLookuper(map[string]string{
			"CONFIG": `{
				"port": "8080",
				"user": "yoyo",
				"max": 51
			}`,
		}),
	}); err != nil {
		panic(err) // TODO: handle error
	}

	fmt.Printf("port: %v\n", s.Config.Port)
	fmt.Printf("user: %v\n", s.Config.User)
	fmt.Printf("max: %v\n", s.Config.Max)

	// Output:
	// port: 8080
	// user: yoyo
	// max: 51
}
