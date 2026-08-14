package users

import (
	"strings"
	"testing"
)

func TestParseUsersIncludesPrimaryAndSupplementaryGroups(t *testing.T) {
	groups, err := parseGroups(strings.NewReader("staff:x:100:alice\nmedia:x:200:alice,bob\n"))
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseUsers(strings.NewReader("alice:x:1000:100::/share/homes/alice:/bin/sh\nbob:x:1001:200::/share/homes/bob:/bin/sh\n"), groups)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || strings.Join(items[0].Groups, ",") != "media,staff" || strings.Join(items[1].Groups, ",") != "media" {
		t.Fatalf("unexpected users: %#v", items)
	}
}
