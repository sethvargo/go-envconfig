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

import "context"

// Mutator is the interface for a mutator function. Mutators act like middleware
// and alter values for subsequent processing. This is useful if you want to
// mutate the environment variable value before it's converted to the proper
// type.
//
// Mutators are only called on defined values (or when decodeunset is true).
type Mutator interface {
	// EnvMutate is called to alter the environment variable value.
	//
	//   - `originalKey` is the unmodified environment variable name as it was defined
	//     on the struct.
	//
	//   - `resolvedKey` is the fully-resolved environment variable name, which may
	//     include prefixes or modifications from processing. When there are
	//     no modifications, this will be equivalent to `originalKey`.
	//
	//   - `originalValue` is the unmodified environment variable's value before any
	//     mutations were run.
	//
	//   - `currentValue` is the currently-resolved value, which may have been
	//     modified by previous mutators and may be modified in the future by
	//     subsequent mutators in the stack.
	//
	// The function returns (in order):
	//
	//   - The new value to use in both future mutations and final processing.
	//
	//   - A boolean which indicates whether future mutations in the stack should be
	//     applied.
	//
	//   - Any errors that occurred.
	//
	EnvMutate(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (newValue string, stop bool, err error)
}

var _ Mutator = (MutatorFunc)(nil)

// MutatorFunc implements the [Mutator] and provides a quick way to create an
// anonymous function.
type MutatorFunc func(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (newValue string, stop bool, err error)

// EnvMutate implements [Mutator].
func (m MutatorFunc) EnvMutate(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (newValue string, stop bool, err error) {
	return m(ctx, originalKey, resolvedKey, originalValue, currentValue)
}

// LegacyMutatorFunc is a helper that eases the transition from the previous
// MutatorFunc signature. It wraps the previous-style mutator function and
// returns a new one. Since the former mutator function had less data, this is
// inherently lossy.
//
// Deprecated: Use [MutatorFunc] instead.
func LegacyMutatorFunc(fn func(ctx context.Context, key, value string) (string, error)) MutatorFunc {
	return func(ctx context.Context, originalKey, resolvedKey, originalValue, currentValue string) (newValue string, stop bool, err error) {
		v, err := fn(ctx, originalKey, currentValue)
		return v, true, err
	}
}
