package games

import (
	"reflect"
	"strings"
	"testing"
)

func TestLaunchFieldsPreserveLiteralArgv(t *testing.T) {
	expected := []string{"space and 中文", "", "$HOME", "a;b", "$(touch x)", `C:\Games\demo`}
	args, env, err := ParseLaunchFields(`["space and 中文","","$HOME","a;b","$(touch x)","C:\\Games\\demo"]`, `{"Z":"value=with=equals","A":"中文"}`)
	if err != nil || !reflect.DeepEqual(args, expected) || !reflect.DeepEqual(env, []string{"A=中文", "Z=value=with=equals"}) {
		t.Fatalf("argv/env changed: %q %q %v", args, env, err)
	}
	args, _, err = ParseLaunchFields(`'space and 中文' "" '$HOME' a\;b '$(touch x)' 'C:\Games\demo'`, "")
	if err != nil || !reflect.DeepEqual(args, expected) {
		t.Fatalf("legacy quotes changed: %q %v", args, err)
	}
	args, _, err = ParseLaunchFields(`"C:\Games\demo"`, "")
	if err != nil || !reflect.DeepEqual(args, []string{`C:\Games\demo`}) {
		t.Fatalf("quoted Windows path changed: %q %v", args, err)
	}
	for _, entry := range []struct{ args, env string }{{`[1]`, ""}, {`[null]`, ""}, {`'unclosed`, ""}, {"trail\\", ""}, {"", `{"omni_save_dir":"escape"}`}, {"", `{"BAD-KEY":"x"}`}, {"", `{"KEY":1}`}, {"", `{"KEY":null}`}, {`["\u0000"]`, ""}, {strings.Repeat("x", 32769), ""}, {"", `null`}} {
		if _, _, err := ParseLaunchFields(entry.args, entry.env); err == nil {
			t.Fatalf("bad launch metadata accepted: %q %q", entry.args, entry.env)
		}
	}
}
