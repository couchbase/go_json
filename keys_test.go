//  Copyright (c) 2017 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package json

import (
	"bytes"
	"reflect"
	"testing"
)

var keysDoc = []byte("{ \"f1\": \"1\", \"f2\": 2, \"f3\": { \"a\": 3 }, \"f4\": [ 4 ], \"f1234567890123456789012345678901234567890\": 33, \"f5\": 5 }")
var keysTests = []struct {
	field string
	res   string
}{
	{"f1", "\"1\""},
	{"f2", "2"},
	{"f3", "{ \"a\": 3 }"},
	{"f4", "[ 4 ]"},
	{"f5", "5"},
}

var scanTests = []struct {
	field string
	res   string
	val   interface{}
}{
	{"f1", "\"1\"", "1"},
	{"f2", "2", int64(2)},
	{"f3", "{ \"a\": 3 }", map[string]interface{}{"a": int64(3)}},
	{"f4", "[ 4 ]", []interface{}{int64(4)}},
	{"f1234567890123456789012345678901234567890", "33", int64(33)},
	{"f5", "5", int64(5)},
}

// tests

func TestFindKey(t *testing.T) {
	res, err := FindKey([]byte("[ null ]"), "null")
	if err != nil {
		t.Fatalf("null array element got %v", err)
	}
	if res != nil {
		t.Fatal("mixing null field and null value")
	}

	res, err = FindKey([]byte("[ \"f1\" ]"), "f1")
	if err != nil {
		t.Fatalf("array element got %v", err)
	}
	if res != nil {
		t.Fatal("mixing field and array element")
	}

	res, err = FindKey([]byte("[ \"a\" ]"), "a")
	if err != nil {
		t.Fatalf("field null got %v", err)
	}
	if res != nil {
		t.Fatal("looked for field, found string")
	}

	for _, test := range keysTests {
		res, err := FindKey(keysDoc, test.field)
		if err != nil {
			t.Fatalf("field %q got %v", test.field, err)
		}

		stringRes := string(res)
		if stringRes != test.res {
			t.Fatalf("field %q expected %q found %q", test.field, test.res, stringRes)
		}
	}
	field := "f99"

	res, err = FindKey(keysDoc, field)
	if err != nil {
		t.Fatalf("field %q got %v", field, err)
	}
	if res != nil {
		t.Fatalf("field %q expected nothing found %q", field, res)
	}
}

func TestFindKeyWithState(t *testing.T) {
	var state KeyState

	SetKeyState(&state, []byte("[ null ]"))
	res, err := state.FindKey("null")
	if err != nil {
		t.Fatalf("null array element  got %v", err)
	}
	if res != nil {
		t.Fatal("mixing null field and null value")
	}
	state.Release()

	SetKeyState(&state, []byte("[ \"f1\" ]"))
	res, err = state.FindKey("f1")
	if err != nil {
		t.Fatalf("array element got %v", err)
	}
	if res != nil {
		t.Fatal("mixing field and array element")
	}
	state.Release()

	SetKeyState(&state, []byte("[ \"a\" ]"))
	res, err = state.FindKey("a")
	if err != nil {
		t.Fatalf("field null got %v", err)
	}
	if res != nil {
		t.Fatal("looked for field, found string")
	}
	state.Release()

	// scan forward
	SetKeyState(&state, keysDoc)
	for _, test := range keysTests {
		res, err := state.FindKey(test.field)
		if err != nil {
			t.Fatalf("field %q got %q", test.field, err)
		}

		stringRes := string(res)
		if stringRes != test.res {
			t.Fatalf("field %q expected %q found %q", test.field, test.res, stringRes)
		}
	}
	field := "f99"

	res, err = state.FindKey(field)
	if err != nil {
		t.Fatalf("field %q got %q", field, err)
	}

	stringRes := string(res)
	if stringRes != "" {
		t.Fatalf("field %q expected nothing found %q", field, stringRes)
	}
	state.Release()

	// scan backwards
	SetKeyState(&state, keysDoc)
	res, err = state.FindKey(field)
	if err != nil {
		t.Fatalf("field %q got %q", field, err)
	}

	stringRes = string(res)
	if stringRes != "" {
		t.Fatalf("field %q expected nothing found %q", field, stringRes)
	}

	offset := state.scan.offset
	for _, test := range keysTests {
		res, err := state.FindKey(test.field)
		if err != nil {
			t.Fatalf("field %q got %q", test.field, err)
		}

		stringRes := string(res)
		if stringRes != test.res {
			t.Fatalf("field %q expected %q found %q", field, test.res, stringRes)
		}
		if offset != state.scan.offset {
			t.Fatalf("field %q was not cached", test.field)
		}
	}
}

func TestScanKeys(t *testing.T) {
	var state ScanState

	// scan keys skipping values
	SetScanState(&state, keysDoc)
	for _, test := range scanTests {
		res, err := state.ScanKeys()
		if err != nil {
			t.Fatalf("key %q got %q", test.field, err)
		}

		if string(res) != test.field {
			t.Fatalf("expected key %q found %q", test.field, res)
		}
	}

	res, err := state.ScanKeys()
	if !state.EOS() {
		t.Fatalf("Did not complete key scan")
	}
	state.Release()

	// check that we can only get a value after a key
	SetScanState(&state, keysDoc)
	res, err = state.ScanKeys()
	if err != nil {
		t.Fatalf("key %q got %q", scanTests[0].field, err)
	}
	if string(res) != scanTests[0].field {
		t.Fatalf("expected key %q found %q", scanTests[0].field, res)
	}
	val, err := state.NextValue()
	if err != nil {
		t.Fatalf("value for key %q got %q", scanTests[0].field, err)
	}
	stringVal := string(val)
	if stringVal != scanTests[0].res {
		t.Fatalf("value for key %q got %q expected %q", scanTests[0].field, stringVal, scanTests[0].res)
	}

	val, err = state.NextValue()
	if err == nil {
		t.Fatalf("Expected error, got nothing")
	}
	if val != nil {
		t.Fatalf("Expected no value , got %v", val)
	}
	state.Release()

	// scan keys checking values
	SetScanState(&state, keysDoc)
	for _, test := range scanTests {
		res, err := state.ScanKeys()
		if err != nil {
			t.Fatalf("key %q got %q", test.field, err)
		}

		if string(res) != test.field {
			t.Fatalf("expected key %q found %q", test.field, res)
		}
		val, err := state.NextValue()
		if err != nil {
			t.Fatalf("value for key %q got %q", test.field, err)
		}
		stringVal := string(val)
		if stringVal != test.res {
			t.Fatalf("value for key %q got %q expected %q", test.field, stringVal, test.res)
		}
	}
	state.Release()

	// the same, but unmarshaling
	// check that we can only get a value after a key
	SetScanState(&state, keysDoc)
	res, err = state.ScanKeys()
	if err != nil {
		t.Fatalf("key %q got %q", scanTests[0].field, err)
	}
	if string(res) != scanTests[0].field {
		t.Fatalf("expected key %q found %q", scanTests[0].field, res)
	}
	unVal, err := state.NextUnmarshaledValue()
	if err != nil {
		t.Fatalf("value for key %q got %q", scanTests[0].field, err)
	}
	if unVal != scanTests[0].val {
		t.Fatalf("value for key %q got %q expected %q", scanTests[0].field, unVal, scanTests[0].val)
	}

	unVal, err = state.NextUnmarshaledValue()
	if err == nil {
		t.Fatalf("Expected error, got nothing")
	}
	if unVal != nil {
		t.Fatalf("Expected no value , got %v", unVal)
	}
	state.Release()

	// scan keys checking values
	SetScanState(&state, keysDoc)
	for _, test := range scanTests {
		res, err := state.ScanKeys()
		if err != nil {
			t.Fatalf("key %q got %q", test.field, err)
		}

		if string(res) != test.field {
			t.Fatalf("expected key %q found %q", test.field, res)
		}
		unVal, err := state.NextUnmarshaledValue()
		if err != nil {
			t.Fatalf("value for key %q got %q", test.field, err)
		}
		if !reflect.DeepEqual(unVal, test.val) {
			t.Fatalf("value for key %q got %v expected %v", test.field, unVal, test.val)
		}
	}
}

// benchmarks

var keytests = []struct {
	path string
	exp  interface{}
}{
	{"", obj},
	{"foo", []interface{}{"bar", "baz"}},
	{"", int64(0)},
	{"a~1b", int64(1)},
	{"c%d", int64(2)},
	{"e^f", int64(3)},
	{"g|h", int64(4)},
	{"i\\j", int64(5)},
	{"k\"l", int64(6)},
	{" ", int64(7)},
	{"m~0n", int64(8)},
	{"g~1n~1r", "has slash, will travel"},
	{"g/n/r", "where's tito?"},
}

func BenchmarkFindKey(b *testing.B) {
	obj := []byte(objSrc)

	b.SetBytes(int64(len(obj)))
	for i := 0; i < b.N; i++ {
		for _, test := range keytests {
			FindKey(obj, test.path)
		}
	}
}

func BenchmarkStateFindKey(b *testing.B) {
	var state KeyState

	obj := []byte(objSrc)
	b.SetBytes(int64(len(obj)))
	SetKeyState(&state, obj)
	for i := 0; i < b.N; i++ {
		for _, test := range keytests {
			state.FindKey(test.path)
		}
	}
}

func BenchmarkStateFindKeyRepeat(b *testing.B) {
	var state KeyState

	obj := []byte(objSrc)
	b.SetBytes(int64(len(obj)))
	for i := 0; i < b.N; i++ {
		SetKeyState(&state, obj)
		for _, test := range keytests {
			state.FindKey(test.path)
		}
		state.Release()
	}
}

func BenchmarkScanKeys(b *testing.B) {
	obj := []byte(objSrc)
	b.SetBytes(int64(len(obj)))
	for i := 0; i < b.N; i++ {
		var state ScanState

		SetScanState(&state, obj)
		for {
			res, _ := state.ScanKeys()
			if res == nil {
				break
			}
			state.NextUnmarshaledValue()
		}
		state.Release()
	}
}

func BenchmarkScanKeysCode(b *testing.B) {
	if codeJSON == nil {
		b.StopTimer()
		codeInit()
		b.StartTimer()
	}

	b.SetBytes(int64(len(codeJSON)))
	for i := 0; i < b.N; i++ {
		var state ScanState

		SetScanState(&state, codeJSON)
		for {
			res, _ := state.ScanKeys()
			if res == nil {
				break
			}
			state.NextUnmarshaledValue()
		}
		state.Release()
	}
}

func BenchmarkScanKeysBig(b *testing.B) {
	if jsonBig == nil {
		b.StopTimer()
		initBig()
		b.StartTimer()
	}
	b.SetBytes(int64(len(jsonBig)))
	for i := 0; i < b.N; i++ {
		var state ScanState

		SetScanState(&state, jsonBig)
		for {
			res, _ := state.ScanKeys()
			if res == nil {
				break
			}
			state.NextUnmarshaledValue()
		}
		state.Release()
	}
}

func BenchmarkDecodeMoreCode(b *testing.B) {
	if codeJSON == nil {
		b.StopTimer()
		codeInit()
		b.StartTimer()
	}
	b.SetBytes(int64(len(codeJSON)))
	for i := 0; i < b.N; i++ {
		var val interface{}

		decoder := NewDecoder(bytes.NewReader(codeJSON))

		// check that it's an object
		token, _ := decoder.Token()
		del, ok := token.(Delim)
		if ok && del.String() == "{" {

			// parse the object
			for decoder.More() {
				_, err := decoder.Token() // key
				if err != nil {
					b.Fatalf("DecodeMoreBig: key decode got %v", err)
				}
				err = decoder.Decode(&val) // value
				if err != nil {
					b.Fatalf("DecodeMoreBig: value decode got %v", err)
				}
			}
		}
	}
}

func BenchmarkDecodeMoreBig(b *testing.B) {
	if jsonBig == nil {
		b.StopTimer()
		initBig()
		b.StartTimer()
	}
	b.SetBytes(int64(len(jsonBig)))
	for i := 0; i < b.N; i++ {
		var val interface{}

		decoder := NewDecoder(bytes.NewReader(jsonBig))

		// check that it's an object
		token, _ := decoder.Token()
		del, ok := token.(Delim)
		if ok && del.String() == "{" {

			// parse the object
			for decoder.More() {
				_, err := decoder.Token() // key
				if err != nil {
					b.Fatalf("DecodeMoreBig: key decode got %v", err)
				}
				err = decoder.Decode(&val) // value
				if err != nil {
					b.Fatalf("DecodeMoreBig: value decode got %v", err)
				}
			}
		}
	}
}
