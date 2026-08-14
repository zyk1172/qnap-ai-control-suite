package system

import (
	"strings"
	"testing"
)

func TestParseNTP(t *testing.T) {
	servers := parseNTP(strings.NewReader("# comment\nserver time.example.test iburst\npool pool.example.test\nserver time.example.test\n"))
	if strings.Join(servers, ",") != "time.example.test,pool.example.test" {
		t.Fatalf("unexpected servers: %#v", servers)
	}
}

func TestParseSocketsDecodesIPv4Address(t *testing.T) {
	input := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n   0: 0100007F:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000  100        0 12345 1\n"
	items := parseSockets("tcp", strings.NewReader(input))
	if len(items) != 1 || items[0].Local != "127.0.0.1:22" || items[0].Remote != "0.0.0.0:0" || items[0].State != "0A" || items[0].Inode != "12345" {
		t.Fatalf("unexpected sockets: %#v", items)
	}
}
