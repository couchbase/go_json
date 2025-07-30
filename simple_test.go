//  Copyright (c) 2020 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

// SimpleUnmarshal() tests and benchmarks - to check for consistency with Unmarshal()

package json

import (
	"bytes"
	"reflect"
	"testing"
)

// tests

func TestSimpleUnmarshalMarshal(t *testing.T) {
	if jsonBig == nil {
		initBig()
	}

	v, err := SimpleUnmarshal(jsonBig)
	if err != nil {
		t.Fatalf("SimpleUnmarshal: %v", err)
	}
	b, err := Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(jsonBig, b) {
		t.Errorf("Marshal jsonBig")
		diff(t, b, jsonBig)
		return
	}
}

func TestSimpleUnmarshalInterface(t *testing.T) {
	v, err := SimpleUnmarshal([]byte(`{"X":1}`))
	if err != nil {
		t.Fatalf("SimpleUnmarshal: %v", err)
	}

	m, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("SimpleUnmarshal did not find a map")
	}
	x := m["X"]
	i, ok := x.(int64)
	if x == nil || !ok || i != 1 {
		t.Fatalf("SimpleUnmarshal did not find X")
	}
}

var simpleUnmarshalTests = []unmarshalTest{
	// basic types
	{in: `true`, out: true},
	{in: `1`, out: int64(1)},
	{in: `1.2`, out: 1.2},
	{in: `-5`, out: int64(-5)},
	{in: `-1.2`, out: -1.2},
	{in: `"a\u1234"`, out: "a\u1234"},
	{in: `"http:\/\/"`, out: "http://"},
	{in: `"g-clef: \uD834\uDD1E"`, out: "g-clef: \U0001D11E"},
	{in: `"invalid: \uD834x\uDD1E"`, out: "invalid: \uFFFDx\uFFFD"},
	{in: "null", out: nil},

	{in: `{"X": [1,2,3], "Y": 4}`, out: map[string]interface{}{"X": []interface{}{int64(1), int64(2), int64(3)}, "Y": int64(4)}},
	{in: `{"x": 1}`, out: map[string]interface{}{"x": int64(1)}},

	// raw values with whitespace
	{in: "\n true ", out: true},
	{in: "\t 1 ", out: int64(1)},
	{in: "\r 1.2 ", out: 1.2},
	{in: "\t -5 \n", out: int64(-5)},
	{in: "\t \"a\\u1234\" \n", ptr: new(string), out: "a\u1234"},

	// syntax errors
	{in: `nulll`, err: &SyntaxError{"invalid character 'l' after top-level value", 5}},
	{in: `nul1`, err: &SyntaxError{"invalid character '1' in literal null (expecting 'l')", 4}},
	{in: `nul`, err: &SyntaxError{"unexpected end of JSON input", 3}},
	{in: `mull`, err: &SyntaxError{"invalid character 'm' looking for beginning of value", 1}},
	{in: `truee`, err: &SyntaxError{"invalid character 'e' after top-level value", 5}},
	{in: `tru3`, err: &SyntaxError{"invalid character '3' in literal true (expecting 'e')", 4}},
	{in: `tru`, err: &SyntaxError{"unexpected end of JSON input", 3}},
	{in: `falsee`, err: &SyntaxError{"invalid character 'e' after top-level value", 6}},
	{in: `fals3`, err: &SyntaxError{"invalid character '3' in literal false (expecting 'e')", 5}},
	{in: `fals`, err: &SyntaxError{"unexpected end of JSON input", 4}},
	{in: `00`, err: &SyntaxError{"invalid character '0' after top-level value", 2}},
	{in: `.0`, err: &SyntaxError{"invalid character '.' looking for beginning of value", 1}},
	{in: `"aaa`, err: &SyntaxError{"unexpected end of JSON input", 4}},
	{in: `{"X": "foo", "Y"}`, err: &SyntaxError{"invalid character '}' after object key", 17}},
	{in: `{"X": "foo", "Y"}`, err: &SyntaxError{"invalid character '}' after object key", 17}},
	{in: `[1, 2, 3+]`, err: &SyntaxError{"invalid character '+' after array element", 9}},
	{in: `{"X":12x}`, err: &SyntaxError{"invalid character 'x' after object key:value pair", 8}, useNumber: true},
	{in: `{"X":12} {"Y":13}`, err: &SyntaxError{"invalid character '{' after top-level value", 10}, useNumber: true},

	// raw value errors
	{in: "\x01 42", err: &SyntaxError{"invalid character '\\x01' looking for beginning of value", 1}},
	{in: " 42 \x01", err: &SyntaxError{"invalid character '\\x01' after top-level value", 5}},
	{in: "\x01 true", err: &SyntaxError{"invalid character '\\x01' looking for beginning of value", 1}},
	{in: " false \x01", err: &SyntaxError{"invalid character '\\x01' after top-level value", 8}},
	{in: "\x01 1.2", err: &SyntaxError{"invalid character '\\x01' looking for beginning of value", 1}},
	{in: " 3.4 \x01", err: &SyntaxError{"invalid character '\\x01' after top-level value", 6}},
	{in: "\x01 \"string\"", err: &SyntaxError{"invalid character '\\x01' looking for beginning of value", 1}},
	{in: " \"string\" \x01", err: &SyntaxError{"invalid character '\\x01' after top-level value", 11}},

	// array tests
	{in: `[1, 2, 3]`, out: []interface{}{int64(1), int64(2), int64(3)}},

	// empty array to interface test
	{in: `[]`, out: []interface{}{}},
	{in: `{"T":[]}`, out: map[string]interface{}{"T": []interface{}{}}},
	{in: `{"T":null}`, out: map[string]interface{}{"T": interface{}(nil)}},

	// integer-keyed map test
	{
		in:  `{"-1":"a","0":"b","1":"c"}`,
		out: map[string]interface{}{"-1": "a", "0": "b", "1": "c"},
	},
	{
		in:  `{"0":"a","10":"c","9":"b"}`,
		out: map[string]interface{}{"0": "a", "9": "b", "10": "c"},
	},
	{
		in:  `{"-9223372036854775808":"min","9223372036854775807":"max"}`,
		out: map[string]interface{}{"-9223372036854775808": "min", "9223372036854775807": "max"},
	},
	{
		in:  `{"18446744073709551615":"max"}`,
		out: map[string]interface{}{"18446744073709551615": "max"},
	},
	{
		in:  `{"0":false,"10":true}`,
		out: map[string]interface{}{"0": false, "10": true},
	},

	// invalid UTF-8 is coerced to valid UTF-8.
	{
		in:  "\"hello\xffworld\"",
		out: "hello\ufffdworld",
	},
	{
		in:  "\"hello\xc2\xc2world\"",
		out: "hello\ufffd\ufffdworld",
	},
	{
		in:  "\"hello\xc2\xffworld\"",
		out: "hello\ufffd\ufffdworld",
	},
	{
		in:  "\"hello\\ud800world\"",
		out: "hello\ufffdworld",
	},
	{
		in:  "\"hello\\ud800\\ud800world\"",
		out: "hello\ufffd\ufffdworld",
	},
	{
		in:  "\"hello\\ud800\\ud800world\"",
		out: "hello\ufffd\ufffdworld",
	},
	{
		in:  "\"hello\xed\xa0\x80\xed\xb0\x80world\"",
		out: "hello\ufffd\ufffd\ufffd\ufffd\ufffd\ufffdworld",
	},

	// large numbers
	{
		// MB-51629
		in:  "-5106534569952410475",
		out: int64(-5106534569952410475),
	},
	{
		// MB-54941
		in:  "18446744073709551610",
		out: float64(18446744073709552000),
	},
	{
		// MB-67849
		in:  "23456789012345678901",
		out: float64(23456789012345680000),
	},
	{
		// MaxInt64
		in:  "9223372036854775807",
		out: int64(9223372036854775807),
	},
	{
		// MaxInt64 + 1
		in:  "9223372036854775808",
		out: float64(9223372036854775807),
	},
	{
		// MinInt64
		in:  "-9223372036854775808",
		out: int64(-9223372036854775808),
	},
	{
		// MinInt64 - 1
		in:  "-9223372036854775809",
		out: float64(-9223372036854775808),
	},
	{
		// MinInt64 * 10
		in:  "-92233720368547758080",
		out: float64(-92233720368547760000),
	},
}

func TestSimpleUnmarshal(t *testing.T) {
	for i, tt := range simpleUnmarshalTests {
		val, err := SimpleUnmarshal([]byte(tt.in))
		if err == nil {
			if tt.err != nil {
				t.Fatalf("Test %v %q was expecting error %v", i, tt.in, tt.err)
			}
			if !reflect.DeepEqual(val, tt.out) {
				t.Fatalf("Test %v %q was expecting %v %T, got %v %T", i, tt.in, tt.out, tt.out, val, val)
			}
		} else {
			if tt.err == nil {
				t.Fatalf("Test %v %q error %v", i, tt.in, err)
			}
			if !reflect.DeepEqual(err, tt.err) {
				t.Fatalf("Test %v %q was expecting err %v, got %v", i, tt.in, tt.err, err)
			}
		}
	}
}

// benchmarks

func BenchmarkSimpleUnmarshal(b *testing.B) {
	if codeJSON == nil {
		b.StopTimer()
		codeInit()
		b.StartTimer()
	}

	b.SetBytes(int64(len(codeJSON)))
	for i := 0; i < b.N; i++ {
		_, err := SimpleUnmarshal(codeJSON)
		if err != nil {
			b.Fatal("SimpleUnmarshal:", err)
		}
	}
}

func BenchmarkSimpleUnmarshalBig(b *testing.B) {
	if jsonBig == nil {
		b.StopTimer()
		initBig()
		b.StartTimer()
	}

	b.SetBytes(int64(len(jsonBig)))
	for i := 0; i < b.N; i++ {
		_, err := SimpleUnmarshal(jsonBig)
		if err != nil {
			b.Fatal("SimpleUnmarshal:", err)
		}
	}
}
