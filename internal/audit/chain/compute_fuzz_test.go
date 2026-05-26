package chain

import (
	"encoding/hex"
	"strings"
	"testing"
)

func FuzzCompute_Determinism(f *testing.F) {

	f.Add("", "promote.note", []byte("payload"), int64(1714435200))
	f.Add(strings.Repeat("0", 64), "deny.cmd", []byte{0x00, 0x01, 0x02}, int64(1))
	f.Add(strings.Repeat("f", 64), "operator.intent.submit", []byte("{\"x\":1}"), int64(2_147_483_647))
	f.Add("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", "session.start", []byte{}, int64(9_999_999_999))

	f.Add("not-hex-not-64-chars", "promote.note", []byte("payload"), int64(1714435200))
	f.Add("", "", []byte("payload"), int64(1))
	f.Add("", "promote.note", []byte("payload"), int64(0))
	f.Add("", "promote.note", []byte("payload"), int64(-1))
	f.Add(strings.Repeat("F", 64), "uppercase.prev", []byte("payload"), int64(1))

	f.Fuzz(func(t *testing.T, prevHash, eventType string, payload []byte, ts int64) {
		h1, err1 := Compute(prevHash, eventType, payload, ts)
		h2, err2 := Compute(prevHash, eventType, payload, ts)

		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("non-deterministic error presence: err1=%v err2=%v", err1, err2)
		}
		if err1 != nil && err2 != nil {
			if err1.Error() != err2.Error() {
				t.Fatalf("non-deterministic error: %q vs %q", err1, err2)
			}

			if h1 != "" || h2 != "" {
				t.Fatalf("error path returned non-empty hash: %q / %q", h1, h2)
			}
			return
		}

		if h1 != h2 {
			t.Fatalf("non-deterministic hash: %q vs %q (inputs prevHash=%q eventType=%q payload=%x ts=%d)",
				h1, h2, prevHash, eventType, payload, ts)
		}

		if len(h1) != 64 {
			t.Fatalf("hash length = %d, want 64 (hash=%q)", len(h1), h1)
		}
		if _, decErr := hex.DecodeString(h1); decErr != nil {
			t.Fatalf("hash not valid hex: %q (err=%v)", h1, decErr)
		}
		if h1 != strings.ToLower(h1) {
			t.Fatalf("hash not lowercase: %q", h1)
		}
	})
}

func FuzzCompute_NoPanic(f *testing.F) {

	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte("|"))
	f.Add([]byte("||||"))
	f.Add(make([]byte, 1024*1024))
	f.Add([]byte{0xff, 0xfe, 0xfd, 0xfc})
	f.Add([]byte("eventType|inject|more|fields|"))

	f.Fuzz(func(t *testing.T, payload []byte) {

		const validPrev = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
		const validType = "promote.note"
		const validTs = int64(1714435200)

		h, err := Compute(validPrev, validType, payload, validTs)
		if err != nil {
			t.Fatalf("unexpected error on valid inputs (payload=%x): %v", payload, err)
		}
		if len(h) != 64 {
			t.Fatalf("hash length = %d, want 64", len(h))
		}
	})
}
