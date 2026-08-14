package network

import (
	"reflect"
	"testing"
)

func TestCommandArgs(t *testing.T) {
	tests := []struct {
		action, iface, value, gateway string
		metric                        int
		want                          []string
	}{
		{"set_mtu", "eth0", "9000", "", 0, []string{"link", "set", "dev", "eth0", "mtu", "9000"}},
		{"set_state", "eth0", "down", "", 0, []string{"link", "set", "dev", "eth0", "down"}},
		{"address_add", "eth0", "192.0.2.10/24", "", 0, []string{"addr", "add", "192.0.2.10/24", "dev", "eth0"}},
		{"route_add", "eth0", "default", "192.0.2.1", 10, []string{"route", "add", "default", "via", "192.0.2.1", "dev", "eth0", "metric", "10"}},
	}
	for _, test := range tests {
		got, err := CommandArgs(test.action, test.iface, test.value, test.gateway, test.metric)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s: got=%v err=%v want=%v", test.action, got, err, test.want)
		}
	}
}

func TestCommandArgsRejectsUnsafeInput(t *testing.T) {
	for _, test := range [][4]string{{"set_state", "eth0;rm", "up", ""}, {"address_add", "eth0", "not-cidr", ""}, {"route_add", "eth0", "default", "not-ip"}} {
		if _, err := CommandArgs(test[0], test[1], test[2], test[3], 0); err == nil {
			t.Fatalf("expected error for %#v", test)
		}
	}
}
