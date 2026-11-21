package mail

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf16"
)

// IMAP folder names travel in modified UTF-7 (RFC 3501 section 5.1.3):
// printable ASCII as itself, & as "&-", and anything else as UTF-16BE in
// base64 with "," for "/" and no padding, between & and -
var utf7 = base64.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+,").WithPadding(base64.NoPadding)

func encodeFolder(name string) string {
	var b strings.Builder
	var run []rune
	flush := func() {
		if len(run) == 0 {
			return
		}
		units := utf16.Encode(run)
		buf := make([]byte, 0, 2*len(units))
		for _, u := range units {
			buf = append(buf, byte(u>>8), byte(u))
		}
		b.WriteByte('&')
		b.WriteString(utf7.EncodeToString(buf))
		b.WriteByte('-')
		run = run[:0]
	}
	for _, r := range name {
		switch {
		case r == '&':
			flush()
			b.WriteString("&-")
		case r >= 0x20 && r <= 0x7e:
			flush()
			b.WriteRune(r)
		default:
			run = append(run, r)
		}
	}
	flush()
	return b.String()
}

func decodeFolder(s string) (string, error) {
	var b strings.Builder
	for len(s) > 0 {
		i := strings.IndexByte(s, '&')
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		s = s[i+1:]
		j := strings.IndexByte(s, '-')
		if j < 0 {
			return "", fmt.Errorf("the folder name has an unterminated &: %q", s)
		}
		if j == 0 {
			b.WriteByte('&')
			s = s[1:]
			continue
		}
		buf, err := utf7.DecodeString(s[:j])
		if err != nil || len(buf)%2 != 0 {
			return "", fmt.Errorf("the folder name has bad modified UTF-7: %q", s[:j])
		}
		units := make([]uint16, len(buf)/2)
		for k := range units {
			units[k] = uint16(buf[2*k])<<8 | uint16(buf[2*k+1])
		}
		b.WriteString(string(utf16.Decode(units)))
		s = s[j+1:]
	}
	return b.String(), nil
}
