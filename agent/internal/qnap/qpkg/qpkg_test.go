package qpkg

import (
	"reflect"
	"testing"
)

func TestCommandArgsUsesQTSFlags(t *testing.T) {
	tests := []struct {
		name, action, path, url string
		want                    []string
	}{
		{"ContainerStation", "start", "", "", []string{"--start", "ContainerStation"}},
		{"ContainerStation", "stop", "", "", []string{"--stop", "ContainerStation"}},
		{"ContainerStation", "status", "", "", []string{"--status", "ContainerStation"}},
		{"", "install_file", "/share/Public/example.qpkg", "", []string{"--manually", "/share/Public/example.qpkg"}},
		{"", "install_url", "", "https://example.invalid/example.qpkg", []string{"--url", "https://example.invalid/example.qpkg"}},
		{"", "update_all", "", "", []string{"--update_all"}},
	}
	for _, test := range tests {
		got, err := CommandArgs(test.name, test.action, test.path, test.url)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s: got=%v err=%v want=%v", test.action, got, err, test.want)
		}
	}
}

func TestCommandArgsRejectsMissingOrUnknownInput(t *testing.T) {
	for _, test := range [][4]string{{"", "start", "", ""}, {"", "install_file", "", ""}, {"pkg", "unknown", "", ""}} {
		if _, err := CommandArgs(test[0], test[1], test[2], test[3]); err == nil {
			t.Fatalf("expected failure for %#v", test)
		}
	}
}
