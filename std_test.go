//  Copyright (c) 2026 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

// Tests for what go_json keeps on top of encoding/json

package json

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
)

var numberTests = []struct {
	in  string
	out any
}{
	{"1", int64(1)},
	{"-0", int64(0)},
	{"9223372036854775807", int64(9223372036854775807)},
	{"-9223372036854775807", int64(-9223372036854775807)},
	{"-9223372036854775808", float64(-9223372036854775808)},
	{"9223372036854775808", float64(9223372036854775808)},
	{"-9223372036854775809", float64(-9223372036854775808)},
	{"1.5", 1.5},
	{"1e2", float64(100)},
	{"1E2", float64(100)},
	{`[1,2.5,-9223372036854775808]`, []any{int64(1), 2.5, float64(-9223372036854775808)}},
	{`{"a":-9223372036854775807,"b":[0.5]}`, map[string]any{"a": int64(-9223372036854775807), "b": []any{0.5}}},
}

func TestUnmarshalNumbers(t *testing.T) {
	for _, tt := range numberTests {
		var v any
		if err := Unmarshal([]byte(tt.in), &v); err != nil {
			t.Errorf("Unmarshal(%s) returned error %v", tt.in, err)
		} else if !reflect.DeepEqual(v, tt.out) {
			t.Errorf("Unmarshal(%s) = %#v, expected %#v", tt.in, v, tt.out)
		}

		// through the Decoder, and nested in a struct, so not taking the scanner based path
		var s struct{ V any }
		if err := NewDecoder(bytes.NewReader([]byte(`{"V":` + tt.in + `}`))).Decode(&s); err != nil {
			t.Errorf("Decode(%s) returned error %v", tt.in, err)
		} else if !reflect.DeepEqual(s.V, tt.out) {
			t.Errorf("Decode(%s) = %#v, expected %#v", tt.in, s.V, tt.out)
		}
	}
}

func TestUnmarshalNumberOverflow(t *testing.T) {
	for _, tt := range []struct {
		in  string
		out any
	}{
		{"1e400", nil},
		{"[1e400,2]", []any{nil, int64(2)}},
		{`{"a":-1E400}`, map[string]any{"a": nil}},
	} {
		var v any
		var ute *UnmarshalTypeError

		err := Unmarshal([]byte(tt.in), &v)
		if !errors.As(err, &ute) {
			t.Errorf("Unmarshal(%s) returned error %T %v, expected *UnmarshalTypeError", tt.in, err, err)
		}
		if !reflect.DeepEqual(v, tt.out) {
			t.Errorf("Unmarshal(%s) = %#v, expected %#v", tt.in, v, tt.out)
		}
	}
}

func TestUnmarshalPointerInInterface(t *testing.T) {
	n := 1
	var v any = &n
	if err := Unmarshal([]byte("5"), &v); err != nil || n != 5 || v != &n {
		t.Errorf("Unmarshal(5) into &int in interface: n = %d, v = %#v, err = %v", n, v, err)
	}

	m := map[string]any{"x": int64(1)}
	v = &m
	if err := Unmarshal([]byte(`{"a":2}`), &v); err != nil || v != &m ||
		!reflect.DeepEqual(m, map[string]any{"x": int64(1), "a": int64(2)}) {
		t.Errorf("Unmarshal({\"a\":2}) into &map in interface: m = %#v, v = %#v, err = %v", m, v, err)
	}

	// a non-pointer is replaced
	v = "abc"
	if err := Unmarshal([]byte("5"), &v); err != nil || v != int64(5) {
		t.Errorf("Unmarshal(5) into string in interface: v = %#v, err = %v", v, err)
	}
}

func TestUnmarshalExisting(t *testing.T) {
	m := map[string]any{"x": int64(1)}
	if err := Unmarshal([]byte(`{"a":2}`), &m); err != nil ||
		!reflect.DeepEqual(m, map[string]any{"x": int64(1), "a": int64(2)}) {
		t.Errorf("Unmarshal into existing map: m = %#v, err = %v", m, err)
	}

	var nm map[string]any
	if err := Unmarshal([]byte(`{"a":2}`), &nm); err != nil || !reflect.DeepEqual(nm, map[string]any{"a": int64(2)}) {
		t.Errorf("Unmarshal into nil map: m = %#v, err = %v", nm, err)
	}

	var a []any
	if err := Unmarshal([]byte(`[1,"b"]`), &a); err != nil || !reflect.DeepEqual(a, []any{int64(1), "b"}) {
		t.Errorf("Unmarshal into nil slice: a = %#v, err = %v", a, err)
	}

	// the wrong kind of document for the target
	if err := Unmarshal([]byte(`[1]`), &nm); err == nil {
		t.Errorf("Unmarshal([1]) into map: expected an error")
	}
}

func TestDecoderUseNumber(t *testing.T) {
	d := NewDecoder(bytes.NewReader([]byte(`{"a":1.50} [12345678901234567890]`)))
	d.UseNumber()

	var v1, v2 any
	if err := d.Decode(&v1); err != nil || !reflect.DeepEqual(v1, map[string]any{"a": Number("1.50")}) {
		t.Errorf("Decode() = %#v, %v", v1, err)
	}
	if err := d.Decode(&v2); err != nil || !reflect.DeepEqual(v2, []any{Number("12345678901234567890")}) {
		t.Errorf("Decode() = %#v, %v", v2, err)
	}
	if err := d.Decode(&v2); err != io.EOF {
		t.Errorf("Decode() at the end returned %v, expected io.EOF", err)
	}
}

func TestDecoderToken(t *testing.T) {
	d := NewDecoder(bytes.NewReader([]byte(`{"a":[1,2.5,true,null,"s"]}`)))
	expected := []Token{Delim('{'), "a", Delim('['), int64(1), 2.5, true, nil, "s", Delim(']'), Delim('}')}
	for _, e := range expected {
		tok, err := d.Token()
		if err != nil || !reflect.DeepEqual(tok, e) {
			t.Errorf("Token() = %#v, %v, expected %#v", tok, err, e)
		}
	}
	if _, err := d.Token(); err != io.EOF {
		t.Errorf("Token() at the end returned %v, expected io.EOF", err)
	}
}

// a bytes.Buffer can be written to after the Decoder has read from it
func TestDecoderBuffer(t *testing.T) {
	var bb bytes.Buffer
	d := NewDecoder(&bb)

	var v1, v2 any
	bb.WriteString(`{"a":1} `)
	if err := d.Decode(&v1); err != nil || !reflect.DeepEqual(v1, map[string]any{"a": int64(1)}) {
		t.Errorf("Decode() = %#v, %v", v1, err)
	}
	bb.WriteString(`[2]`)
	if err := d.Decode(&v2); err != nil || !reflect.DeepEqual(v2, []any{int64(2)}) {
		t.Errorf("Decode() after a write = %#v, %v", v2, err)
	}
}

func TestValidate(t *testing.T) {
	for _, tt := range []struct {
		in     string
		msg    string
		offset int64
	}{
		{`{"a":1}`, "", 0},
		{`{"a":}`, "invalid character '}' looking for beginning of value", 6},
		{`[1,2`, "unexpected end of JSON input", 4},
		{`{"a" 1}`, "invalid character '1' after object key", 6},
		{`1 2`, "invalid character '2' after top-level value", 3},
	} {
		err := Validate([]byte(tt.in))
		if tt.msg == "" {
			if err != nil {
				t.Errorf("Validate(%s) returned error %v", tt.in, err)
			}
			continue
		}
		se, ok := err.(*SyntaxError)
		if !ok || se.msg != tt.msg || se.Offset != tt.offset {
			t.Errorf("Validate(%s) = %#v, expected %q at %d", tt.in, err, tt.msg, tt.offset)
		}
	}
}

// strings are encoded as go_json always has, rather than as encoding/json does
func TestMarshalString(t *testing.T) {
	for _, tt := range []struct {
		in, out, html string
	}{
		{"abc", `"abc"`, `"abc"`},
		{"a\"b\\c", `"a\"b\\c"`, `"a\"b\\c"`},
		{"\t\n\r", `"\t\n\r"`, `"\t\n\r"`},
		{"a\bb\fc\x00\x1f", `"a\u0008b\u000cc\u0000\u001f"`, `"a\u0008b\u000cc\u0000\u001f"`},
		{"x\xffy\xe2\x82", `"x\ufffdy\ufffd\ufffd"`, `"x\ufffdy\ufffd\ufffd"`},
		{"a\u2028b\u2029c", `"a\u2028b\u2029c"`, `"a\u2028b\u2029c"`},
		{"<a&b>", `"<a&b>"`, `"\u003ca\u0026b\u003e"`},
		{"\u00e9\u4e16\u754c\U0001f600", "\"\u00e9\u4e16\u754c\U0001f600\"", "\"\u00e9\u4e16\u754c\U0001f600\""},
	} {
		if b, err := MarshalNoEscape(tt.in); err != nil || string(b) != tt.out {
			t.Errorf("MarshalNoEscape(%q) = %s, %v, expected %s", tt.in, b, err, tt.out)
		}

		var buf bytes.Buffer
		buf.WriteString("x")
		if err := MarshalNoEscapeToBuffer(tt.in, &buf); err != nil || buf.String() != "x"+tt.out {
			t.Errorf("MarshalNoEscapeToBuffer(%q) = %s, %v, expected x%s", tt.in, buf.String(), err, tt.out)
		}

		buf.Reset()
		if err := MarshalStringToBuffer(tt.in, &buf); err != nil || buf.String() != tt.html {
			t.Errorf("MarshalStringToBuffer(%q) = %s, %v, expected %s", tt.in, buf.String(), err, tt.html)
		}
	}
}

type rawMarshaler string

func (r rawMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(r), nil
}

// strings anywhere in a value are encoded as go_json always has, and the
// output of MarshalJSON methods is not HTML escaped
func TestMarshalNested(t *testing.T) {
	const s = "a\bb\x01<\xff"
	const html = `"a\u0008b\u0001\u003c\ufffd"`
	const noHTML = `"a\u0008b\u0001<\ufffd"`
	type st struct {
		S string `json:"s"`
		N int    `json:"n,string"`
		A any    `json:"a"`
	}
	type namedString string
	type named struct {
		F namedString `json:"<f>"`
		M map[namedString]namedString
	}
	for _, tt := range []struct {
		in           any
		html, noHTML string
	}{
		{s, html, noHTML},
		{[]any{s, int64(1), 2.5, true, nil}, "[" + html + `,1,2.5,true,null]`, "[" + noHTML + `,1,2.5,true,null]`},
		{map[string]any{s: s, "b": map[string]any{}, "a": []any{}}, `{"a":[],` + html + ":" + html + `,"b":{}}`,
			`{"a":[],` + noHTML + ":" + noHTML + `,"b":{}}`},
		{map[string]string{s: s}, "{" + html + ":" + html + "}", "{" + noHTML + ":" + noHTML + "}"},
		{[]string{s}, "[" + html + "]", "[" + noHTML + "]"},
		{st{S: s, N: 7, A: map[string]any{"x": s}}, `{"s":` + html + `,"n":"7","a":{"x":` + html + `}}`,
			`{"s":` + noHTML + `,"n":"7","a":{"x":` + noHTML + `}}`},

		// MarshalJSON output, as query relies on for its values, named string
		// types and struct field names are not HTML escaped
		{map[string]any{"m": rawMarshaler(`{"k<" : ["x<y&", "\u0008"]}`)}, `{"m":{"k<":["x<y&","\u0008"]}}`,
			`{"m":{"k<":["x<y&","\u0008"]}}`},
		{named{F: "<a&b>", M: map[namedString]namedString{"<k>": "&"}}, `{"<f>":"<a&b>","M":{"<k>":"&"}}`,
			`{"<f>":"<a&b>","M":{"<k>":"&"}}`},
		{[]any{namedString("<n>")}, `["<n>"]`, `["<n>"]`},
		{[]any(nil), "null", "null"},
		{map[string]any(nil), "null", "null"},
	} {
		if b, err := Marshal(tt.in); err != nil || string(b) != tt.html {
			t.Errorf("Marshal(%#v) = %s, %v, expected %s", tt.in, b, err, tt.html)
		}
		if b, err := MarshalNoEscape(tt.in); err != nil || string(b) != tt.noHTML {
			t.Errorf("MarshalNoEscape(%#v) = %s, %v, expected %s", tt.in, b, err, tt.noHTML)
		}

		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		if err := enc.Encode(tt.in); err != nil || buf.String() != tt.html+"\n" {
			t.Errorf("Encode(%#v) = %s, %v, expected %s", tt.in, buf.String(), err, tt.html)
		}
		buf.Reset()
		enc.SetEscapeHTML(false)
		if err := enc.Encode(tt.in); err != nil || buf.String() != tt.noHTML+"\n" {
			t.Errorf("Encode(%#v) without HTML escaping = %s, %v, expected %s", tt.in, buf.String(), err, tt.noHTML)
		}
	}
}

func TestMarshalIndent(t *testing.T) {
	in := map[string]any{"a": []any{int64(1), "<"}, "b": map[string]any{}}
	expected := "{\n>\t\"a\": [\n>\t\t1,\n>\t\t\"\\u003c\"\n>\t],\n>\t\"b\": {}\n>}"
	if b, err := MarshalIndent(in, ">", "\t"); err != nil || string(b) != expected {
		t.Errorf("MarshalIndent() = %s, %v, expected %s", b, err, expected)
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetIndent(">", "\t")
	if err := enc.Encode(in); err != nil || buf.String() != expected+"\n" {
		t.Errorf("Encode() with SetIndent = %s, %v, expected %s", buf.String(), err, expected)
	}
}

func TestMarshalErrors(t *testing.T) {
	for _, in := range []any{math.NaN(), []any{math.Inf(1)}, map[string]any{"c": make(chan int)}, rawMarshaler(`{bad`)} {
		if b, err := Marshal(in); err == nil {
			t.Errorf("Marshal(%#v) = %s, expected an error", in, b)
		}
	}

	// a cycle is stopped by the nesting limit, rather than overflowing the stack
	m := map[string]any{}
	m["m"] = m
	if _, err := Marshal(m); err == nil {
		t.Errorf("Marshal() of a cyclic map: expected an error")
	}
}

// More reports invalid input after a value, so that Decode returns the error
func TestDecoderMore(t *testing.T) {
	for _, tt := range []struct {
		in    string
		more  []bool
		error bool
	}{
		{``, []bool{false}, false},
		{`1 "s" [2]`, []bool{true, true, true, false}, false},
		{`{"a":1} x`, []bool{true, true}, true},
		{`[1] ,`, []bool{true, true}, true},
		{`{"a":1} }`, []bool{true, false}, false},
		{`{"a":1} ]`, []bool{true, false}, false},
	} {
		d := NewDecoder(bytes.NewReader([]byte(tt.in)))
		var err error
		for i, m := range tt.more {
			if more := d.More(); more != m {
				t.Errorf("%q: More() %d = %v, expected %v", tt.in, i, more, m)
			}
			if !m {
				break
			}
			var v any
			if err = d.Decode(&v); err != nil {
				break
			}
		}
		if (err != nil) != tt.error {
			t.Errorf("%q: Decode() returned error %v", tt.in, err)
		}
	}
}

// syntax errors are go_json's own SyntaxError, as before
func TestSyntaxError(t *testing.T) {
	for _, tt := range []struct {
		in     string
		msg    string
		offset int64
	}{
		{`{"a":}`, "invalid character '}' looking for beginning of value", 6},
		{`[1,2`, "unexpected end of JSON input", 4},
		{`"a\x"`, "invalid character 'x' in string escape code", 4},
		{`{"A":1,}`, "invalid character '}' looking for beginning of object key string", 8},
		{``, "unexpected end of JSON input", 0},
	} {
		var v any
		var s struct{ A int }
		for _, err := range []error{Unmarshal([]byte(tt.in), &v), Unmarshal([]byte(tt.in), &s)} {
			var se *SyntaxError
			if !errors.As(err, &se) || se.msg != tt.msg || se.Offset != tt.offset {
				t.Errorf("Unmarshal(%s) returned %T %v, expected %q at %d", tt.in, err, err, tt.msg, tt.offset)
			}
		}
	}

	var v any
	var se *SyntaxError
	if err := NewDecoder(bytes.NewReader([]byte(`{"a":}`))).Decode(&v); !errors.As(err, &se) {
		t.Errorf("Decode() returned %T %v, expected *SyntaxError", err, err)
	}
	if err := NewDecoder(bytes.NewReader([]byte(`[1,2`))).Decode(&v); err != io.ErrUnexpectedEOF {
		t.Errorf("Decode() of a truncated value returned %T %v, expected io.ErrUnexpectedEOF", err, err)
	}
}

// the scanner based decoder Unmarshal uses for interface values must accept
// exactly what encoding/json does, and give the same values
func FuzzUnmarshalAny(f *testing.F) {
	for _, s := range []string{`{"a":[1,2.5,-0,"s\u00e9\ud83d\ude00",true,false,null],"b":{}}`, `[]`, `-9223372036854775808`,
		`"\'"`, `"\ud800"`, `"\ud800\u0041"`, "\"\xff\xfe\"", `{"a":1,"a":2}`, `1e400`, `[1,]`, `{"a":1}x`, ` 1 `, `"\u00"`,
		`{"k":{"k":{"k":[[[]]]}}}`, `0.1e-2`, `01`, `-`, `1.`, `.5`, `tru`, `nul`, `"\x"`, "\"a\x01\"", `{"a" 1}`, `{,}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		val, serr := simpleUnmarshal(data)
		ok := serr == nil
		var exp any
		err := jsonv2.Unmarshal(data, &exp, unmarshalOptions)
		if ok && err != nil {
			t.Fatalf("%q: accepted, encoding/json returned %v", data, err)
		}
		if ok && !reflect.DeepEqual(val, exp) {
			t.Fatalf("%q: = %#v, encoding/json gave %#v", data, val, exp)
		}
		if !ok && err == nil {

			// not wrong, as Unmarshal falls back to encoding/json, but slower
			t.Logf("%q: rejected, encoding/json accepted", data)
		}
	})
}

type marshalError struct{}

func (marshalError) MarshalJSON() ([]byte, error) {
	return nil, errors.New("failed")
}

// a string field with the ,string option is encoded as by Marshal, then encoded again
func TestMarshalStringOption(t *testing.T) {
	s := "a\b<"
	in := struct {
		S string  `json:"s,string"`
		P *string `json:"p,string"`
		N int     `json:"n,string"`
	}{S: s, P: &s, N: 3}
	expected := `{"s":"\"a\\u0008\\u003c\"","p":"\"a\\u0008\\u003c\"","n":"3"}`
	for _, marshal := range []func(any) ([]byte, error){Marshal, MarshalNoEscape} {
		b, err := marshal(in)
		if err != nil || string(b) != expected {
			t.Errorf("Marshal() = %s, %v, expected %s", b, err, expected)
		}
		out := in
		out.S, out.P, out.N = "", nil, 0
		if err = Unmarshal(b, &out); err != nil || out.S != s || out.P == nil || *out.P != s || out.N != 3 {
			t.Errorf("Unmarshal(%s) = %+v, %v", b, out, err)
		}
	}
}

// an error from within a map[string]any or []any is not wrapped by them
func TestMarshalErrorUnwrapped(t *testing.T) {
	for _, in := range []any{[]any{marshalError{}}, map[string]any{"a": []any{map[string]any{"b": marshalError{}}}}} {
		_, err := Marshal(in)
		var me *MarshalerError
		if !errors.As(err, &me) || me.Type != reflect.TypeFor[*marshalError]() || strings.Count(err.Error(), "error calling") != 1 || !strings.HasSuffix(err.Error(), ": failed") {
			t.Errorf("Marshal(%#v) returned %v", in, err)
		}
	}
}

// the offset of a number that does not fit is after it, within the document
// for Unmarshal, and within the stream for a Decoder
func TestNumberErrorOffset(t *testing.T) {
	for _, tt := range []struct {
		doc    string
		offset int64
	}{{`1e400`, 5}, {`  1e400`, 7}, {`[1, 1e400]`, 9}, {`{"a": 1e400}`, 11}} {
		var v any
		var terr *UnmarshalTypeError
		if err := Unmarshal([]byte(tt.doc), &v); !errors.As(err, &terr) || terr.Offset != tt.offset {
			t.Errorf("Unmarshal(%s) returned %v, expected offset %d", tt.doc, err, tt.offset)
		}
		d := NewDecoder(strings.NewReader(`"s" ` + tt.doc))
		d.Decode(&v)
		if err := d.Decode(&v); !errors.As(err, &terr) || terr.Offset != tt.offset+4 {
			t.Errorf("Decode(%s) returned %v, expected offset %d", tt.doc, err, tt.offset+4)
		}
	}
}

// Unmarshal into an interface value keeps to encoding/json's nesting limit,
// while Validate, as before, has none
func TestNestingLimit(t *testing.T) {
	for _, tt := range []struct {
		depth int
		ok    bool
	}{{10000, true}, {10001, false}} {
		doc := []byte(strings.Repeat("[", tt.depth) + strings.Repeat("]", tt.depth))
		var v any
		var a []any
		var r RawMessage
		for _, dst := range []any{&v, &a, &r} {
			var serr *SyntaxError
			if err := Unmarshal(doc, dst); (err == nil) != tt.ok || (err != nil && !errors.As(err, &serr)) {
				t.Errorf("Unmarshal() into %T, %d levels, returned %#v", dst, tt.depth, err)
			}
		}
		if err := Validate(doc); err != nil {
			t.Errorf("Validate(), %d levels, returned %v", tt.depth, err)
		}
	}
}

// Decode into an interface value takes the same scanner based path as Unmarshal
func TestDecoderAny(t *testing.T) {
	d := NewDecoder(strings.NewReader(`{"a":[1,-9223372036854775808,2.5]} "s" 1e400 [1] {"a":}`))
	for _, expected := range []any{map[string]any{"a": []any{int64(1), float64(-9223372036854775808), 2.5}}, "s"} {
		var v any
		if err := d.Decode(&v); err != nil || !reflect.DeepEqual(v, expected) {
			t.Errorf("Decode() = %#v, %v, expected %#v", v, err, expected)
		}
	}

	var v any
	var ute *UnmarshalTypeError
	if err := d.Decode(&v); !errors.As(err, &ute) || v != nil {
		t.Errorf("Decode() of 1e400 = %#v, %v, expected *UnmarshalTypeError", v, err)
	}

	// a pointer held in the interface value is decoded into
	a := []any{}
	v = &a
	if err := d.Decode(&v); err != nil || v != &a || !reflect.DeepEqual(a, []any{int64(1)}) {
		t.Errorf("Decode() into a pointer in an interface value: a = %#v, v = %#v, %v", a, v, err)
	}

	var se *SyntaxError
	if err := d.Decode(&v); !errors.As(err, &se) {
		t.Errorf("Decode() returned %T %v, expected *SyntaxError", err, err)
	}
}

// Token errors are as Decode's: io.ErrUnexpectedEOF for truncated input, and
// go_json's SyntaxError otherwise
func TestTokenErrors(t *testing.T) {
	for _, tt := range []struct {
		doc string
		err error
	}{{`[1 x`, &SyntaxError{}}, {`{"a" 1}`, &SyntaxError{}}, {`]`, &SyntaxError{}}, {`[1, `, io.ErrUnexpectedEOF},
		{`"abc`, io.ErrUnexpectedEOF}, {`[tru`, io.ErrUnexpectedEOF}, {` `, io.EOF}} {
		d := NewDecoder(strings.NewReader(tt.doc))
		var err error
		for err == nil {
			_, err = d.Token()
		}
		var serr *SyntaxError
		if _, ok := tt.err.(*SyntaxError); ok && !errors.As(err, &serr) || !ok && err != tt.err {
			t.Errorf("Token() on %s returned %#v", tt.doc, err)
		}
	}
}

// a nil pointer target is an InvalidUnmarshalError, as in encoding/json
func TestUnmarshalNilPointer(t *testing.T) {
	for _, v := range []any{(*any)(nil), (*map[string]any)(nil), (*[]any)(nil)} {
		var ierr *InvalidUnmarshalError
		if err := Unmarshal([]byte(`[1]`), v); !errors.As(err, &ierr) {
			t.Errorf("Unmarshal() into %T returned %v", v, err)
		}
		if err := NewDecoder(strings.NewReader(`[1]`)).Decode(v); !errors.As(err, &ierr) {
			t.Errorf("Decode() into %T returned %v", v, err)
		}
	}
}

// a map or slice that contains itself is an UnsupportedValueError, as in
// encoding/json, even past a deep acyclic part
func TestMarshalCycle(t *testing.T) {
	m := map[string]any{}
	m["self"] = m
	a := []any{nil}
	a[0] = a
	deep := any(m)
	for range 2000 {
		deep = []any{map[string]any{"a": deep}}
	}
	type withMap struct{ M map[string]any }
	viaPointer := map[string]any{}
	viaPointer["p"] = &withMap{M: viaPointer}
	viaStruct := map[string]any{}
	viaStruct["s"] = withMap{M: viaStruct}
	fromPointer := &withMap{}
	fromPointer.M = map[string]any{"p": fromPointer}
	viaValue := map[string]any{}
	viaValue["v"] = pointerMarshaler{M: viaValue}
	for _, v := range []any{m, a, deep, viaPointer, viaStruct, fromPointer, viaValue} {
		var uerr *UnsupportedValueError
		if _, err := Marshal(v); !errors.As(err, &uerr) || !strings.Contains(err.Error(), "encountered a cycle via") {
			t.Errorf("Marshal() of a cycle returned %v", err)
		}
	}

	// values shared within a value are walked once: 2^1500 paths, 1500 values
	var dag any = map[string]any{"a": 1}
	for range 1500 {
		dag = []any{dag, dag}
	}
	if err := checkCycle(dag); err != nil {
		t.Errorf("checkCycle() of a shared acyclic value returned %v", err)
	}

	// what Marshal does not walk is not a cycle: here an unexported parent
	type node struct {
		Kids   []any
		parent *node
	}
	root := &node{}
	for n, i := root, 0; i < 1500; i++ {
		kid := &node{parent: n}
		n.Kids = []any{map[string]any{"k": kid}}
		n = kid
	}
	if _, err := Marshal(root); err != nil {
		t.Errorf("Marshal() of a deep tree with parent pointers returned %v", err)
	}

	// nor is a cycle in an unexported embedded field that is not a struct
	type wrapper struct {
		hiddenMap
		Next any
	}
	hidden := hiddenMap{}
	hidden["self"] = hidden
	var embedded any = wrapper{hiddenMap: hidden}
	for range 1100 {
		embedded = []any{embedded}
	}
	if _, err := Marshal(embedded); err != nil {
		t.Errorf("Marshal() of a cycle in an unexported embedded map returned %v", err)
	}

	// MarshalJSON on the pointer is called for a pointer, which ends the walk
	viaMethod := map[string]any{}
	viaMethod["p"] = &pointerMarshaler{M: viaMethod}
	if b, err := Marshal(viaMethod); err != nil || string(b) != `{"p":1}` {
		t.Errorf("Marshal() of a cycle through MarshalJSON = %s, %v", b, err)
	}
}

type hiddenMap map[string]any

// not called on a value that is not addressable, such as one in a map
type pointerMarshaler struct{ M map[string]any }

func (*pointerMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`1`), nil
}

// Decode into a new map or array of interface values also takes the scanner
// based path, and numbers are int64, as with Unmarshal
func TestDecoderMapSlice(t *testing.T) {
	d := NewDecoder(strings.NewReader(`{"a":[1,2.5]} [3,{"b":4}] [5] {"c":6}`))
	var m map[string]any
	var a []any
	if err := d.Decode(&m); err != nil || !reflect.DeepEqual(m, map[string]any{"a": []any{int64(1), 2.5}}) {
		t.Errorf("Decode() into map = %#v, %v", m, err)
	}
	if err := d.Decode(&a); err != nil || !reflect.DeepEqual(a, []any{int64(3), map[string]any{"b": int64(4)}}) {
		t.Errorf("Decode() into []any = %#v, %v", a, err)
	}
	var m2 map[string]any
	var terr *UnmarshalTypeError
	if err := d.Decode(&m2); !errors.As(err, &terr) {
		t.Errorf("Decode() of an array into a map returned %v", err)
	}
	var a2 []any
	if err := d.Decode(&a2); !errors.As(err, &terr) {
		t.Errorf("Decode() of an object into []any returned %v", err)
	}
}

// strings are encoded by appendString, whichever way they are written
func FuzzMarshalString(f *testing.F) {
	for _, s := range []string{"", "plain", "a\"b\\c", "<a&b>", "\x00\x1f\x7f", "\u2028\u2029", "\xff\xfe", "\u00e9\U0001f600", "a\tb\nc\rd"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, escapeHTML := range []bool{true, false} {
			expected := "[" + string(appendString(nil, s, escapeHTML)) + `,{` + string(appendString(nil, s, escapeHTML)) + `:1}]`
			marshal := Marshal
			if !escapeHTML {
				marshal = MarshalNoEscape
			}
			if b, err := marshal([]any{s, map[string]any{s: 1}}); err != nil || string(b) != expected {
				t.Fatalf("%q: = %s, %v, expected %s", s, b, err, expected)
			}
		}
	})
}

// Token's syntax errors have encoding/json's wording, as Decode's do; they are
// reworded from jsontext's, so this pins them against a change in its wording
func TestTokenErrorText(t *testing.T) {
	for _, tt := range []struct {
		doc string
		msg string
		off int64
	}{
		{`[1 x`, "invalid character 'x' after array element", 4},
		{`{"a" 1}`, "invalid character '1' after object key", 6},
		{`]`, "invalid character ']' looking for beginning of value", 1},
		{`[1e]`, "invalid character ']' in numeric literal", 4},
		{`{"a":1 "b"}`, "invalid character '\"' after object key:value pair", 8},
	} {
		d := NewDecoder(strings.NewReader(tt.doc))
		var err error
		for err == nil {
			_, err = d.Token()
		}
		var v any
		derr := NewDecoder(strings.NewReader(tt.doc)).Decode(&v)
		serr, ok := err.(*SyntaxError)
		if !ok || serr.msg != tt.msg || serr.Offset != tt.off || derr == nil || derr.Error() != tt.msg {
			t.Errorf("%s: Token() returned %#v, Decode() %v, expected %q at %d", tt.doc, err, derr, tt.msg, tt.off)
		}
	}
}
