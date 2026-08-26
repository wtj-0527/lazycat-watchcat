package collector

import "testing"

func TestParseInvitation(t *testing.T) {
	hub, code, err := ParseInvitation("http://192.168.1.20:18080/#pairing-code=ABCD-1234")
	if err != nil {
		t.Fatal(err)
	}
	if hub != "http://192.168.1.20:18080" || code != "ABCD-1234" {
		t.Fatalf("hub=%q code=%q", hub, code)
	}
	if _, _, err := ParseInvitation("http://192.168.1.20:18080/"); err == nil {
		t.Fatal("expected missing pairing code error")
	}
}
