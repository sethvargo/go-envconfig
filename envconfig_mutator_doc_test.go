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

// MyMutator is a mutator that keeps a count of all mutated values, appending
// the current count to each environment variable that is processed.
type MyMutator struct {
	counter int
}

func (m *MyMutator) EnvMutate(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (newValue string, stop bool, err error) {
	m.counter++
	return fmt.Sprintf("%s-%d", currentValue, m.counter), false, nil
}

func Example_mutator() {
	// This exmaple demonstrates authoring a complex mutator that modifies
	// environment variable values before processing.

	type MyStruct struct {
		FieldA string `env:"FIELD_A"`
		FieldB string `env:"FIELD_B"`
		FieldC string `env:"FIELD_C"`
		FieldD string `env:"FIELD_D"`
	}

	var s MyStruct
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{
		Target: &s,
		Lookuper: envconfig.MapLookuper(map[string]string{
			"FIELD_A": "a",
			"FIELD_B": "b",
			"FIELD_C": "c",
		}),
		Mutators: []envconfig.Mutator{&MyMutator{}},
	}); err != nil {
		panic(err) // TODO: handle error
	}

	fmt.Printf("field a: %q\n", s.FieldA)
	fmt.Printf("field b: %q\n", s.FieldB)
	fmt.Printf("field c: %q\n", s.FieldC)
	fmt.Printf("field d: %q\n", s.FieldD)

	// Output:
	// field a: "a-1"
	// field b: "b-2"
	// field c: "c-3"
	// field d: ""
}
