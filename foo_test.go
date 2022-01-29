package envconfig

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFoo(t *testing.T) {
	cases := []struct {
		name string
		m    map[string]string
		want *Foo
	}{{
		name: "keep_nil_ptr_if_not_touched",
		m:    map[string]string{"X": "xxx"},
		want: &Foo{
			V1: &Bar{X: "xxx"},
		},
	}, {
		name: "ptr_set_if_touched",
		m:    map[string]string{"X": "xxx", "Z": "zzz"},
		want: &Foo{
			V1: &Bar{X: "xxx"},
			V2: &Sub{Z: "zzz"},
		},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got Foo
			err := ProcessWith(context.Background(), &got, MapLookuper(tc.m))
			if err != nil {
				t.Error(err)
			}
			if diff := cmp.Diff(tc.want, &got); diff != "" {
				t.Errorf("(-want,+got):\n%s", diff)
			}
		})
	}
}

type Foo struct {
	V1 *Bar
	V2 *Sub
}

type Bar struct {
	X string `env:"X"`
}

type Sub struct {
	Z string `env:"Z"`
}
