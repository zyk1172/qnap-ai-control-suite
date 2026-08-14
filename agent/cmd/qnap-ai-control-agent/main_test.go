package main

import "testing"

func TestTokenHashDeterministic(t *testing.T) {
	if hashToken("token") != hashToken("token") || hashToken("token") == hashToken("other") {
		t.Fatal("token hash is not deterministic")
	}
}
