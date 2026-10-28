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
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestStructDecoderOverwriteDefault(t *testing.T) {
	type config struct {
		Time    time.Time  `env:"ENVCONFIG_TEST_TIME,overwrite,default=2000-01-01T00:00:00Z"`
		TimePtr *time.Time `env:"ENVCONFIG_TEST_TIME,overwrite,default=2000-01-01T00:00:00Z"`
		URL     url.URL    `env:"ENVCONFIG_TEST_URL,overwrite,default=https://default.example"`
		URLPtr  *url.URL   `env:"ENVCONFIG_TEST_URL,overwrite,default=https://default.example"`
		String  string     `env:"ENVCONFIG_TEST_STRING,overwrite,default=default"`
	}

	value := func(year int, host, str string) config {
		at := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		home := url.URL{Scheme: "https", Host: host}
		return config{Time: at, TimePtr: &at, URL: home, URLPtr: &home, String: str}
	}

	for _, tc := range []struct {
		name        string
		initial     config
		want        config
		setEnv      bool
		decodeUnset bool
	}{
		{
			name:    "existing values beat defaults",
			initial: value(2024, "existing.example", "existing"),
			want:    value(2024, "existing.example", "existing"),
		},
		{
			name: "defaults initialize zero values",
			want: value(2000, "default.example", "default"),
		},
		{
			name:    "provided values overwrite existing values",
			initial: value(2024, "existing.example", "existing"),
			want:    value(2025, "environment.example", "environment"),
			setEnv:  true,
		},
		{
			name:        "decodeunset preserves existing values",
			initial:     value(2024, "existing.example", "existing"),
			want:        value(2024, "existing.example", "existing"),
			decodeUnset: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"ENVCONFIG_TEST_TIME", "ENVCONFIG_TEST_URL", "ENVCONFIG_TEST_STRING"} {
				// Setenv registers restoration before Unsetenv removes the value.
				t.Setenv(key, "")
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			if tc.setEnv {
				t.Setenv("ENVCONFIG_TEST_TIME", "2025-01-01T00:00:00Z")
				t.Setenv("ENVCONFIG_TEST_URL", "https://environment.example")
				t.Setenv("ENVCONFIG_TEST_STRING", "environment")
			}
			got := tc.initial
			if err := ProcessWith(context.Background(), &Config{
				Target:             &got,
				DefaultDecodeUnset: tc.decodeUnset,
			}); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("configuration mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStructDecoderGlobalOverwriteDefault(t *testing.T) {
	type child struct {
		Time *time.Time `env:"TIME,default=2000-01-01T00:00:00Z"`
	}
	c := struct {
		Child child `env:",prefix=ENVCONFIG_TEST_NESTED_"`
	}{Child: child{Time: new(time.Time)}}
	wantPointer := c.Child.Time
	t.Setenv("ENVCONFIG_TEST_NESTED_TIME", "")
	if err := os.Unsetenv("ENVCONFIG_TEST_NESTED_TIME"); err != nil {
		t.Fatal(err)
	}
	if err := ProcessWith(context.Background(), &Config{
		Target:           &c,
		DefaultOverwrite: true,
	}); err != nil {
		t.Fatal(err)
	}
	if c.Child.Time != wantPointer || !c.Child.Time.IsZero() {
		t.Errorf("existing pointer changed: got %v, want the original zero time", c.Child.Time)
	}
}
