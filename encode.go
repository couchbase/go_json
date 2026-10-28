// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package json is encoding/json, with the output, number decoding and scanner
// based helpers that query relies on; see std.go
package json

import (
	"bytes"
	"unicode/utf8"
)

// This is a specialisation of MarshalNoEscapeToBuffer so we can avoid implicit heap allocations when converting for interface{}
// (We can also skip the generic processing and call appendString directly.)
// It is used by stringValue.WriteJSON
func MarshalStringNoEscapeToBuffer(s string, buf *bytes.Buffer) error {
	buf.Write(appendString(buf.AvailableBuffer(), s, false)) // escapeHTML: false
	return nil
}

// This specialisation is the same as the above barring escaping HTML (to match Marshal())
func MarshalStringToBuffer(s string, buf *bytes.Buffer) error {
	buf.Write(appendString(buf.AvailableBuffer(), s, true)) // escapeHTML: true
	return nil
}

var hex = "0123456789abcdef"

// String encoding is kept here rather than delegated to encoding/json, as its
// output is relied on byte for byte (query results, hash and DISTINCT keys):
//   - control characters other than \t, \n and \r are written as \u00XX
//     (encoding/json now writes \b and \f)
//   - invalid UTF-8 is written as the escape \ufffd
//     (encoding/json now writes the U+FFFD character itself)
//   - U+2028 and U+2029 are always escaped, <, > and & optionally

// bytes that need no escaping, with and without HTML escaping; non-ASCII
// bytes are not safe, they need checking for invalid UTF-8, U+2028 and U+2029
var safeSet, htmlSafeSet [256]bool

func init() {
	for b := 0x20; b < utf8.RuneSelf; b++ {
		safe := b != '\\' && b != '"'
		safeSet[b] = safe
		htmlSafeSet[b] = safe && b != '<' && b != '>' && b != '&'
	}
}

// appendString appends the JSON encoding of s to dst
func appendString(dst []byte, s string, escapeHTML bool) []byte {
	safe := &safeSet
	if escapeHTML {
		safe = &htmlSafeSet
	}

	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		for i < len(s) && safe[s[i]] {
			i++
		}
		if i == len(s) {
			break
		}
		if b := s[i]; b < utf8.RuneSelf {
			dst = append(dst, s[start:i]...)
			switch b {
			case '\\', '"':
				dst = append(dst, '\\', b)
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				// This encodes bytes < 0x20 except for \t, \n and \r.
				// If escapeHTML is set, it also escapes <, >, and &
				// because they can lead to security holes when
				// user-controlled strings are rendered into JSON
				// and served to some browsers.
				dst = append(dst, '\\', 'u', '0', '0', hex[b>>4], hex[b&0xF])
			}
			i++
			start = i
			continue
		}
		// a run of non-ASCII runes
		for i < len(s) && s[i] >= utf8.RuneSelf {
			c, size := utf8.DecodeRuneInString(s[i:])
			if c == utf8.RuneError && size == 1 {
				dst = append(dst, s[start:i]...)
				dst = append(dst, `\ufffd`...)
				i += size
				start = i
				continue
			}
			// U+2028 is LINE SEPARATOR.
			// U+2029 is PARAGRAPH SEPARATOR.
			// They are both technically valid characters in JSON strings,
			// but don't work in JSONP, which has to be evaluated as JavaScript,
			// and can lead to security holes there. It is valid JSON to
			// escape them, so we do so unconditionally.
			// See http://timelessrepo.com/json-isnt-a-javascript-subset for discussion.
			if c == '\u2028' || c == '\u2029' {
				dst = append(dst, s[start:i]...)
				dst = append(dst, `\u202`...)
				dst = append(dst, hex[c&0xF])
				i += size
				start = i
				continue
			}
			i += size
		}
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}
