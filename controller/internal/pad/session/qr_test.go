package session

import (
	"encoding/base64"
	"testing"
	"time"
)

// Shared with server/app/models/play_session.rb and
// server/test/models/play_session_test.rb. Key is 32 bytes, time is
// Time.at(1_767_225_600), counter is floor(unix/30) as 8-byte big-endian.
// Ruby OpenSSL HMAC-SHA256 + PlaySession.base32 yields OT56FOOG.
const (
	sharedQRKey = "qr-key-bytes-for-shared-vector!!"
	sharedQRB64 = "cXIta2V5LWJ5dGVzLWZvci1zaGFyZWQtdmVjdG9yISE="
	sharedQRAt  = int64(1767225600)
	sharedQRTok = "OT56FOOG"
)

func TestQRTokenSharedVector(t *testing.T) {
	if base64.StdEncoding.EncodeToString([]byte(sharedQRKey)) != sharedQRB64 {
		t.Fatal("shared key base64 drifted")
	}
	if got := len(sharedQRKey); got != 32 {
		t.Fatalf("key length %d", got)
	}
	got, err := QRToken(sharedQRB64, sharedQRAt)
	if err != nil {
		t.Fatal(err)
	}
	if got != sharedQRTok {
		t.Fatalf("token %s want %s", got, sharedQRTok)
	}
	same, err := QRToken(sharedQRB64, sharedQRAt+29)
	if err != nil || same != sharedQRTok {
		t.Fatalf("same window: %s %v", same, err)
	}
	next, err := QRToken(sharedQRB64, sharedQRAt+30)
	if err != nil {
		t.Fatal(err)
	}
	if next == sharedQRTok || len(next) != 8 {
		t.Fatalf("next window %s", next)
	}
}

func TestBase32RFC4648(t *testing.T) {
	// PlaySession.base32("foobar") == "MZXW6YTBOI"
	encoded := encodeBase32([]byte("foobar"))
	if encoded != "MZXW6YTBOI" {
		t.Fatalf("foobar base32 %s", encoded)
	}
}

func TestQRTokenRejectsBadKey(t *testing.T) {
	if _, err := QRToken("not base64!!!", 0); err == nil {
		t.Fatal("expected decode error")
	}
	if _, err := QRToken("", 0); err == nil {
		t.Fatal("expected empty key error")
	}
}

func TestJoinURL(t *testing.T) {
	got, err := JoinURL("https://yui.example/play/KQXM", "OT56FOOG")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://yui.example/play/KQXM?t=OT56FOOG" {
		t.Fatalf("join url %s", got)
	}
	if _, err := JoinURL("/play/KQXM", "OT56FOOG"); err == nil {
		t.Fatal("expected relative url error")
	}
}

func TestNextQRDeadline(t *testing.T) {
	at := time.Unix(sharedQRAt, 0).UTC()
	if next := nextQRDeadline(at); !next.Equal(at.Add(30 * time.Second)) {
		t.Fatalf("boundary %s", next)
	}
	mid := at.Add(15 * time.Second)
	if next := nextQRDeadline(mid); !next.Equal(at.Add(30 * time.Second)) {
		t.Fatalf("mid %s", next)
	}
}

func encodeBase32(in []byte) string {
	// Same alphabet and bit grouping as PlaySession.base32.
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	bits := make([]byte, 0, len(in)*8)
	for _, b := range in {
		for shift := 7; shift >= 0; shift-- {
			bits = append(bits, (b>>shift)&1)
		}
	}
	out := make([]byte, 0, (len(bits)+4)/5)
	for i := 0; i < len(bits); i += 5 {
		var value byte
		for bit := 0; bit < 5; bit++ {
			value <<= 1
			if i+bit < len(bits) {
				value |= bits[i+bit]
			}
		}
		out = append(out, alphabet[value])
	}
	return string(out)
}
