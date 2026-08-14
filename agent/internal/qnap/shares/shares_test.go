package shares

import (
	"strings"
	"testing"
)

func TestParseNFS(t *testing.T) {
	items := ParseNFS(strings.NewReader("# comment\n/share/Public 192.0.2.0/24(rw) host(ro)\n"))
	if len(items) != 1 || items[0].Path != "/share/Public" || len(items[0].Hosts) != 2 {
		t.Fatalf("%#v", items)
	}
}
