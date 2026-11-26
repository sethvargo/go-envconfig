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

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type presetRequiredField[T any] struct {
	Value T `env:"VALUE,required"`
}

type presetGlobalRequiredField[T any] struct {
	Value T `env:"VALUE"`
}

type presetRequiredDecoder struct {
	Value string
}

func (d *presetRequiredDecoder) EnvDecode(context.Context, string) error {
	return errors.New("prefilled decoder should not run")
}

func TestProcessWith_PresetDecoderRequired(t *testing.T) {
	preset := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	presetURL := url.URL{Scheme: "https", Host: "example.com"}
	cases := []struct {
		name     string
		target   any
		want     any
		required bool
	}{
		{"time", &presetRequiredField[time.Time]{preset}, &presetRequiredField[time.Time]{preset}, false},
		{"time_pointer", &presetRequiredField[*time.Time]{ptrTo(preset)}, &presetRequiredField[*time.Time]{ptrTo(preset)}, false},
		{"url", &presetRequiredField[url.URL]{presetURL}, &presetRequiredField[url.URL]{presetURL}, false},
		{"url_pointer", &presetRequiredField[*url.URL]{ptrTo(presetURL)}, &presetRequiredField[*url.URL]{ptrTo(presetURL)}, false},
		{"custom", &presetRequiredField[presetRequiredDecoder]{presetRequiredDecoder{"preset"}}, &presetRequiredField[presetRequiredDecoder]{presetRequiredDecoder{"preset"}}, false},
		{"custom_pointer", &presetRequiredField[*presetRequiredDecoder]{&presetRequiredDecoder{"preset"}}, &presetRequiredField[*presetRequiredDecoder]{&presetRequiredDecoder{"preset"}}, false},
		{"global_time", &presetGlobalRequiredField[time.Time]{preset}, &presetGlobalRequiredField[time.Time]{preset}, true},
		{"global_time_pointer", &presetGlobalRequiredField[*time.Time]{ptrTo(preset)}, &presetGlobalRequiredField[*time.Time]{ptrTo(preset)}, true},
		{"string_control", &presetRequiredField[string]{"preset"}, &presetRequiredField[string]{"preset"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ProcessWith(context.Background(), &Config{
				Target: tc.target, Lookuper: MapLookuper(nil), DefaultRequired: tc.required,
			}); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, tc.target); diff != "" {
				t.Fatalf("unexpected config (-want +got):\n%s", diff)
			}
		})
	}
}

func TestProcessWith_RequiredDecoderOverwrite(t *testing.T) {
	preset := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	provided := "2024-01-01T00:00:00Z"
	cases := []struct {
		name      string
		value     time.Time
		values    map[string]string
		overwrite bool
		want      time.Time
		wantErr   error
	}{
		{"zero_missing", time.Time{}, nil, false, time.Time{}, ErrMissingRequired},
		{"overwrite_missing", preset, nil, true, preset, ErrMissingRequired},
		{"overwrite_provided", preset, map[string]string{"VALUE": provided}, true, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), nil},
		{"preserve_provided", preset, map[string]string{"VALUE": provided}, false, preset, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := presetRequiredField[time.Time]{tc.value}
			err := ProcessWith(context.Background(), &Config{
				Target: &target, Lookuper: MapLookuper(tc.values), DefaultOverwrite: tc.overwrite,
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
			if !target.Value.Equal(tc.want) {
				t.Fatalf("expected %s, got %s", tc.want, target.Value)
			}
		})
	}
}
