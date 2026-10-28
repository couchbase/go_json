// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package json

import (
	"strconv"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// String unquoting helpers, kept from go_json (originally copied from encoding/json)

// getu4 decodes \uXXXX from the beginning of s, returning the hex value,
// or it returns -1.
func getu4(s []byte) rune {
	if len(s) < 6 || s[0] != '\\' || s[1] != 'u' {
		return -1
	}
	r, err := strconv.ParseUint(string(s[2:6]), 16, 64)
	if err != nil {
		return -1
	}
	return rune(r)
}

func unquoteBytes(s []byte) (t []byte, ok bool) {
	l := len(s)
	if l < 2 {
		return
	}

	start := 0
	end := l - 1
	for start <= end && s[start] == ' ' {
		start++
	}
	for start <= end && s[end] == ' ' {
		end--
	}

	if start >= end {
		return
	}
	if s[start] != '"' || s[end] != '"' {
		return
	}
	start++

	// Check for unusual characters. If there are none,
	// then no unquoting is needed, so return a slice of the
	// original bytes.
	r := start
	for r < end {
		c := s[r]
		if c == '\\' || c == '"' || c < ' ' {
			break
		}
		if c < utf8.RuneSelf {
			r++
			continue
		}
		rr, size := utf8.DecodeRune(s[r:])
		if rr == utf8.RuneError && size == 1 {
			break
		}
		r += size
	}
	if r == end {
		return s[start:end], true
	}

	b := make([]byte, l+2*utf8.UTFMax)
	w := copy(b, s[start:r])
	for r < end {
		// Out of room?  Can only happen if s is full of
		// malformed UTF-8 and we're replacing each
		// byte with RuneError.
		if w >= len(b)-2*utf8.UTFMax {
			nb := make([]byte, (len(b)+utf8.UTFMax)*2)
			copy(nb, b[0:w])
			b = nb
		}
		switch c := s[r]; {
		case c == '\\':
			r++
			if r >= len(s) {
				return
			}
			switch s[r] {
			default:
				return
			case '"', '\\', '/', '\'':
				b[w] = s[r]
				r++
				w++
			case 'b':
				b[w] = '\b'
				r++
				w++
			case 'f':
				b[w] = '\f'
				r++
				w++
			case 'n':
				b[w] = '\n'
				r++
				w++
			case 'r':
				b[w] = '\r'
				r++
				w++
			case 't':
				b[w] = '\t'
				r++
				w++
			case 'u':
				r--
				rr, sz := getu4OrSurrogate(s, r)
				if rr < 0 {
					return
				}
				r += sz
				w += utf8.EncodeRune(b[w:], rr)
			}

		// Quote, control characters are invalid.
		case c == '"', c < ' ':
			return

		// ASCII
		case c < utf8.RuneSelf:
			b[w] = c
			r++
			w++

		// Coerce to well-formed UTF-8.
		default:
			rr, size := utf8.DecodeRune(s[r:])
			r += size
			w += utf8.EncodeRune(b[w:], rr)
		}
	}
	return b[0:w], true
}

func getu4OrSurrogate(s []byte, r int) (rune, int) {
	sz := 0
	rr := getu4(s[r:])
	if rr < 0 {
		return -1, 0
	}
	sz += 6
	r += 6
	if utf16.IsSurrogate(rr) {
		rr1 := getu4(s[r:])
		if dec := utf16.DecodeRune(rr, rr1); dec != unicode.ReplacementChar {

			// A valid pair; consume.
			sz += 6
			return dec, sz
		}

		// Invalid surrogate; fall back to replacement rune.
		rr = unicode.ReplacementChar
	}
	return rr, sz
}
