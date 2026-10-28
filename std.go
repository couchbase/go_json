//  Copyright (c) 2026 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package json

// The standard parts of the API are delegated to encoding/json (Go 1.27, built
// on encoding/json/v2), keeping the one semantic go_json has always had: JSON
// integers decode into int64 rather than float64, when the target is an
// interface value.

import (
	"bytes"
	"encoding"
	"errors"
	"io"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	stdjson "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
)

type Marshaler = stdjson.Marshaler
type Unmarshaler = stdjson.Unmarshaler
type Number = stdjson.Number
type RawMessage = stdjson.RawMessage
type Token = stdjson.Token
type Delim = stdjson.Delim
type InvalidUnmarshalError = stdjson.InvalidUnmarshalError
type UnmarshalTypeError = stdjson.UnmarshalTypeError
type UnsupportedTypeError = stdjson.UnsupportedTypeError
type UnsupportedValueError = stdjson.UnsupportedValueError
type MarshalerError = stdjson.MarshalerError
type InvalidUTF8Error = stdjson.InvalidUTF8Error
type UnmarshalFieldError = stdjson.UnmarshalFieldError

func Marshal(v any) ([]byte, error) {
	b, err := jsonv2.Marshal(v, marshalOptions)
	return b, unwrapMarshalError(err)
}

func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	b, err := Marshal(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err = Indent(&buf, b, prefix, indent); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// An Encoder writes JSON values to an output stream
type Encoder struct {
	w            io.Writer
	err          error
	escapeHTML   bool
	indentPrefix string
	indentValue  string
	indentBuf    bytes.Buffer
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w, escapeHTML: true}
}

// Encode writes the JSON encoding of v to the stream, followed by a newline
func (enc *Encoder) Encode(v any) error {
	if enc.err != nil {
		return enc.err
	}
	opts := marshalOptions
	if !enc.escapeHTML {
		opts = marshalNoEscapeOptions
	}

	// as before, each value is encoded into a new buffer, and only the buffer
	// for indenting is kept
	b, err := jsonv2.Marshal(v, opts)
	if err != nil {
		return unwrapMarshalError(err)
	}
	b = append(b, '\n')
	if enc.indentPrefix != "" || enc.indentValue != "" {
		enc.indentBuf.Reset()
		if err := Indent(&enc.indentBuf, b, enc.indentPrefix, enc.indentValue); err != nil {
			return err
		}
		b = enc.indentBuf.Bytes()
	}
	if _, err = enc.w.Write(b); err != nil {
		enc.err = err
	}
	return err
}

// SetIndent makes subsequent values indented, as by Indent; SetIndent("", "") stops it
func (enc *Encoder) SetIndent(prefix, indent string) {
	enc.indentPrefix = prefix
	enc.indentValue = indent
}

// SetEscapeHTML specifies whether <, > and & are escaped in strings
func (enc *Encoder) SetEscapeHTML(on bool) {
	enc.escapeHTML = on
}

func HTMLEscape(dst *bytes.Buffer, src []byte) {
	stdjson.HTMLEscape(dst, src)
}

func Valid(data []byte) bool {
	return stdjson.Valid(data)
}

// Validate returns nil iff data is valid JSON; the scanner is only used when
// encoding/json finds an error, as encoding/json is quicker at checking
func Validate(data []byte) error {
	if stdjson.Valid(data) {
		return nil
	}

	// the scanner has the last word: it has no nesting limit
	return syntaxError(data)
}

// MarshalNoEscape is like Marshal, but does not escape <, & and >
func MarshalNoEscape(v any) ([]byte, error) {
	if s, ok := v.(string); ok {
		return appendString(make([]byte, 0, len(s)+2), s, false), nil
	}
	var buf bytes.Buffer
	if err := MarshalNoEscapeToBuffer(v, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MarshalNoEscapeToBuffer is like MarshalNoEscape, but writes to buf
func MarshalNoEscapeToBuffer(v any, buf *bytes.Buffer) error {
	if s, ok := v.(string); ok {
		return MarshalStringNoEscapeToBuffer(s, buf)
	}
	l := buf.Len()
	if err := jsonv2.MarshalWrite(buf, v, marshalNoEscapeOptions); err != nil {
		buf.Truncate(l)
		return unwrapMarshalError(err)
	}
	return nil
}

/*
Marshalling keeps go_json's output, which is relied on byte for byte:
  - strings, and the object names of map[string]any, are encoded by appendString
  - the output of MarshalJSON methods is not HTML escaped, as query relies on
    for the bytes of its values, so jsontext's own escaping is turned off and
    appendString does it (go_json's compact escaped some of it but not all,
    depending on what preceded it)

Generic documents (map[string]any and []any) are walked here, which is quicker
than going through encoding/json for each of their values.
*/
var marshalOptions = newMarshalOptions(true)
var marshalNoEscapeOptions = newMarshalOptions(false)

func newMarshalOptions(escapeHTML bool) jsonv2.Options {
	return jsonv2.JoinOptions(stdjson.DefaultOptionsV1(), jsontext.EscapeForHTML(false),
		jsonv2.WithMarshalers(jsonv2.JoinMarshalers(
			jsonv2.MarshalToFunc(func(enc *jsontext.Encoder, s string) error {

				// a field with the ,string option is encoded as by Marshal, and then
				// encoded again, as before
				if quote, _ := jsonv2.GetOption(enc.Options(), jsonv2.StringifyNumbers); quote {
					s = string(appendString(nil, s, true))
				}
				return writeString(enc, s, escapeHTML)
			}),
			jsonv2.MarshalToFunc(func(enc *jsontext.Encoder, m map[string]any) error {
				return marshalMap(enc, m, escapeHTML, false)
			}),
			jsonv2.MarshalToFunc(func(enc *jsontext.Encoder, a []any) error {
				return marshalSlice(enc, a, escapeHTML, false)
			}))))
}

// writeString writes s as appendString encodes it; a string with nothing to
// escape, the common case, is quoted by jsontext, in one pass, rather than
// encoded by appendString and then checked by jsontext
func writeString(enc *jsontext.Encoder, s string, escapeHTML bool) error {
	safe := &safeSet
	if escapeHTML {
		safe = &htmlSafeSet
	}
	for i := 0; i < len(s); i++ {
		if !safe[s[i]] {
			return enc.WriteValue(appendString(enc.AvailableBuffer(), s, escapeHTML))
		}
	}
	return enc.WriteToken(jsontext.String(s))
}

// errors from within map[string]any and []any come back wrapped in a
// MarshalerError for each of them; return the error itself, as before
func unwrapMarshalError(err error) error {
	for {
		me, ok := err.(*MarshalerError)
		if !ok || me.Err == nil || !hookedTypes[me.Type] {
			return err
		}
		err = me.Err
	}
}

// types with a MarshalToFunc hook, as json/v2 reports them
var hookedTypes = map[reflect.Type]bool{
	reflect.TypeFor[*string]():         true,
	reflect.TypeFor[*map[string]any](): true,
	reflect.TypeFor[*[]any]():          true,
}

// acyclic is set within a map or slice that has been checked for a cycle, so
// that its subtree is not checked again
func marshalAny(enc *jsontext.Encoder, v any, escapeHTML, acyclic bool) error {
	switch v := v.(type) {
	case nil:
		return enc.WriteToken(jsontext.Null)
	case string:
		return writeString(enc, v, escapeHTML)
	case bool:
		return enc.WriteToken(jsontext.Bool(v))
	case int64:
		return enc.WriteToken(jsontext.Int(v))
	case int:
		return enc.WriteToken(jsontext.Int(int64(v)))
	case float64:
		if !math.IsInf(v, 0) && !math.IsNaN(v) {
			return enc.WriteToken(jsontext.Float(v))
		}
	case map[string]any:
		return marshalMap(enc, v, escapeHTML, acyclic)
	case []any:
		return marshalSlice(enc, v, escapeHTML, acyclic)
	}
	return jsonv2.MarshalEncode(enc, v)
}

func marshalMap(enc *jsontext.Encoder, m map[string]any, escapeHTML, acyclic bool) error {
	if m == nil {
		return enc.WriteToken(jsontext.Null)
	}
	if !acyclic && inCycleWindow(enc) {
		if err := checkCycle(m); err != nil {
			return err
		}
		acyclic = true
	}

	// the keys of small maps are sorted on the stack
	var buf [16]string
	keys := buf[:0]
	if len(m) > len(buf) {
		keys = make([]string, 0, len(m))
	}
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for _, k := range keys {
		if err := writeString(enc, k, escapeHTML); err != nil {
			return err
		}
		if err := marshalAny(enc, m[k], escapeHTML, acyclic); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}

func marshalSlice(enc *jsontext.Encoder, a []any, escapeHTML, acyclic bool) error {
	if a == nil {
		return enc.WriteToken(jsontext.Null)
	}
	if !acyclic && inCycleWindow(enc) {
		if err := checkCycle(a); err != nil {
			return err
		}
		acyclic = true
	}
	if err := enc.WriteToken(jsontext.BeginArray); err != nil {
		return err
	}
	for _, v := range a {
		if err := marshalAny(enc, v, escapeHTML, acyclic); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndArray)
}

// as encoding/json, maps and slices nested this deep are checked for a cycle,
// which would otherwise only end at the nesting limit; those in a window of
// depths are, so that a cycle through structs or pointers is caught too, but
// not again within one that has been checked
const (
	_CYCLE_DEPTH  = 1000
	_CYCLE_WINDOW = 64
)

func inCycleWindow(enc *jsontext.Encoder) bool {
	d := enc.StackDepth()
	return d >= _CYCLE_DEPTH && d < _CYCLE_DEPTH+_CYCLE_WINDOW
}

// checkCycle returns encoding/json's error if a value within v contains itself
func checkCycle(v any) error {
	if c := findCycle(reflect.ValueOf(v), map[cycleKey]bool{}); c.IsValid() {
		return &UnsupportedValueError{Value: c, Str: "encountered a cycle via " + c.Type().String()}
	}
	return nil
}

// a pointer, map or slice, with its length, as encoding/json identifies them; a
// key is true while its value is being walked, and false once it is acyclic
type cycleKey struct {
	ptr unsafe.Pointer
	len int
	typ reflect.Type
}

var marshalerType = reflect.TypeFor[Marshaler]()
var textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()

// embeddedStruct reports whether f embeds a struct, or a pointer to one, whose
// exported fields encoding/json marshals even if f itself is unexported
func embeddedStruct(f reflect.StructField) bool {
	if !f.Anonymous {
		return false
	}
	t := f.Type
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct
}

// findCycle walks what Marshal would: exported fields, and not into types that
// marshal themselves
func findCycle(v reflect.Value, seen map[cycleKey]bool) reflect.Value {
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return v
	}
	t := v.Type()

	// as encoding/json, methods on the pointer are called only on an
	// addressable value, such as a field of a struct reached through a pointer
	pt := reflect.PointerTo(t)
	if t.Implements(marshalerType) || t.Implements(textMarshalerType) ||
		v.CanAddr() && (pt.Implements(marshalerType) || pt.Implements(textMarshalerType)) {
		return reflect.Value{}
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if v.IsNil() {
			return reflect.Value{}
		}
		k := cycleKey{v.UnsafePointer(), 0, t}
		if v.Kind() == reflect.Slice {
			k.len = v.Len()
		}
		if walking, ok := seen[k]; ok {
			if walking {
				return v
			}
			return reflect.Value{}
		}
		seen[k] = true
		var c reflect.Value
		switch v.Kind() {
		case reflect.Pointer:
			c = findCycle(v.Elem(), seen)
		case reflect.Map:
			for it := v.MapRange(); it.Next() && !c.IsValid(); {
				c = findCycle(it.Value(), seen)
			}
		case reflect.Slice:
			for i := 0; i < v.Len() && !c.IsValid(); i++ {
				c = findCycle(v.Index(i), seen)
			}
		}
		if c.IsValid() {
			return c
		}
		seen[k] = false
	case reflect.Array:
		for i := range v.Len() {
			if c := findCycle(v.Index(i), seen); c.IsValid() {
				return c
			}
		}
	case reflect.Struct:
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() && !embeddedStruct(f) || f.Tag.Get("json") == "-" {
				continue
			}
			if c := findCycle(v.Field(i), seen); c.IsValid() {
				return c
			}
		}
	}
	return reflect.Value{}
}

// options for v1 semantics, with integers decoding into int64 in interface values
var unmarshalOptions = jsonv2.JoinOptions(stdjson.DefaultOptionsV1(),
	jsonv2.WithUnmarshalers(jsonv2.UnmarshalFromFunc(unmarshalAnyNumber(false))))
var unmarshalNumberOptions = jsonv2.JoinOptions(stdjson.DefaultOptionsV1(),
	jsonv2.WithUnmarshalers(jsonv2.UnmarshalFromFunc(unmarshalAnyNumber(true))))

func unmarshalAnyNumber(useNumber bool) func(*jsontext.Decoder, *any) error {
	return func(dec *jsontext.Decoder, v *any) error {

		// as before, a number goes into a pointer held in the interface value
		if dec.PeekKind() != '0' || holdsPointer(*v) {
			return errors.ErrUnsupported
		}
		raw, err := dec.ReadValue()
		if err != nil {
			return err
		}
		if useNumber {
			*v = Number(raw)
			return nil
		}
		n, err := convertNumber(unsafe.String(unsafe.SliceData(raw), len(raw)), dec.InputOffset())
		if err != nil {
			return err
		}
		*v = n
		return nil
	}
}

// holdsPointer reports whether v is a non-nil pointer, which decoding into an
// interface value holding it fills in, rather than replacing
func holdsPointer(v any) bool {
	if v == nil {
		return false
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && !rv.IsNil()
}

var float64Type = reflect.TypeFor[float64]()

// As go_json: integer literals that fit decode into int64, other numbers into float64
func convertNumber(s string, offset int64) (any, error) {
	if strings.IndexAny(s, ".eE") < 0 {
		i, err := strconv.ParseInt(s, 10, 64)
		if err == nil && i > math.MinInt64 {
			return i, nil
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, &UnmarshalTypeError{Value: "number " + s, Type: float64Type, Offset: offset}
	}
	return f, nil
}

func Unmarshal(data []byte, v any) error {

	// a document into an interface value, or a new map or array of them, is the
	// common case: the scanner based decoder is quicker than encoding/json at it
	checked := false
	if simpleTarget(v) {
		switch unmarshalSimple(data, v) {
		case _SIMPLE_DONE:
			return nil

		// syntax errors are go_json's own SyntaxError, as before, with the
		// scanner's message and offset; input that failed the scanner based
		// decoder on its syntax is checked before encoding/json is run on it
		case _SIMPLE_SYNTAX:
			if err := syntaxError(data); err != nil {
				return err
			}
			checked = true
		}
	}
	if err := jsonv2.Unmarshal(data, v, unmarshalOptions); err != nil {
		if serr, ok := err.(*stdjson.SyntaxError); ok {
			if !checked {
				if err := syntaxError(data); err != nil {
					return err
				}
			}

			// what the scanner accepts, such as nesting beyond 10000 levels
			return &SyntaxError{msg: serr.Error(), Offset: serr.Offset}
		}
		return err
	}
	return nil
}

// simpleTarget reports whether the scanner based decoder can decode into v
func simpleTarget(v any) bool {
	switch p := v.(type) {
	case *any:
		return p != nil && !holdsPointer(*p)
	case *map[string]any:
		return p != nil && *p == nil
	case *[]any:
		return p != nil && *p == nil
	}
	return false
}

// what unmarshalSimple did: decoded into v, failed on a syntax error, or
// left it to encoding/json, for a value of the wrong kind or out of range
const (
	_SIMPLE_DONE = iota
	_SIMPLE_SYNTAX
	_SIMPLE_OTHER
)

// unmarshalSimple decodes data into a simpleTarget; a map or array target is
// checked against the first byte, so that a value of another kind is not built
func unmarshalSimple(data []byte, v any) int {
	switch v.(type) {
	case *map[string]any:
		if firstByte(data) != '{' {
			return _SIMPLE_OTHER
		}
	case *[]any:
		if firstByte(data) != '[' {
			return _SIMPLE_OTHER
		}
	}
	val, err := simpleUnmarshal(data)
	if err != nil {
		if _, ok := err.(*SyntaxError); ok {
			return _SIMPLE_SYNTAX
		}
		return _SIMPLE_OTHER
	}
	switch p := v.(type) {
	case *any:
		*p = val
	case *map[string]any:
		*p = val.(map[string]any)
	case *[]any:
		*p = val.([]any)
	}
	return _SIMPLE_DONE
}

func firstByte(data []byte) byte {
	for _, c := range data {
		if !isSpace(c) {
			return c
		}
	}
	return 0
}

// syntaxError returns the scanner's error for invalid data
func syntaxError(data []byte) error {
	scan := newScanner(data)
	defer freeScanner(scan)
	return checkValid(data, scan)
}

// Decode data with the scanner based decoder, strictly, so that it gives what
// encoding/json would; on an error, encoding/json is left to give its result
func simpleUnmarshal(data []byte) (any, error) {
	scan := newScanner(data)
	defer freeScanner(scan)
	scan.reset()
	scan.strict = true
	return unmarshaledValue(scan)
}

// Decoder decodes a stream of JSON values, with go_json's number semantics
type Decoder struct {
	dec       *jsontext.Decoder
	useNumber bool
}

func NewDecoder(r io.Reader) *Decoder {

	// jsontext parses a bytes.Buffer in place and fails if it is written to
	// after a Decode; hide it, so that it can still be used as a pipe, as before
	if bb, ok := r.(*bytes.Buffer); ok {
		r = struct{ io.Reader }{bb}
	}
	return &Decoder{dec: jsontext.NewDecoder(r, stdjson.DefaultOptionsV1())}
}

// UseNumber makes numbers in interface values decode into Number
func (d *Decoder) UseNumber() {
	d.useNumber = true
}

func (d *Decoder) Decode(v any) error {
	// a value into an interface value, or a new map or array of them, is read,
	// and decoded as by Unmarshal; a read error is converted as UnmarshalDecode
	// would, without reading the value again
	opts := unmarshalOptions
	if d.useNumber {
		opts = unmarshalNumberOptions
	} else if simpleTarget(v) {
		raw, err := d.dec.ReadValue()
		if err != nil {
			return readError(err)
		}
		if unmarshalSimple(raw, v) == _SIMPLE_DONE {
			return nil
		}
		err = jsonv2.Unmarshal(raw, v, opts)

		// offsets are within raw; make them within the stream
		base := d.dec.InputOffset() - int64(len(raw))
		switch err := err.(type) {
		case *UnmarshalTypeError:
			err.Offset += base
		case *stdjson.SyntaxError:
			err.Offset += base
		}
		return decoderError(err)
	}
	return decoderError(jsonv2.UnmarshalDecode(d.dec, v, opts))
}

// readError converts an error from reading the stream: a read error is
// returned as it is, as encoding/json does, and others as by decoderError
func readError(err error) error {
	if _, ok := err.(*jsontext.SyntacticError); !ok {
		if rerr := errors.Unwrap(err); rerr != nil {
			return rerr
		}
	}
	return decoderError(err)
}

// as before: a truncated value is io.ErrUnexpectedEOF, and other syntax errors
// are go_json's own SyntaxError
func decoderError(err error) error {
	switch serr := err.(type) {
	case *stdjson.SyntaxError:
		if serr.Error() == _UNEXPECTED_END {
			return io.ErrUnexpectedEOF
		}
		return &SyntaxError{msg: serr.Error(), Offset: serr.Offset}
	case *jsontext.SyntacticError:
		if errors.Is(serr.Err, io.ErrUnexpectedEOF) {
			return io.ErrUnexpectedEOF
		}

		// a read error, which encoding/json returns as it is
		if rerr := errors.Unwrap(serr.Err); rerr != nil {
			return rerr
		}

		// encoding/json's message for it, as for Decode
		msg := serr.Err.Error()
		if i := strings.Index(msg, " (expecting"); i >= 0 && !strings.Contains(msg, " in literal") {
			msg = msg[:i]
		}
		return &SyntaxError{msg: syntaxErrorReplacer.Replace(msg), Offset: serr.ByteOffset}
	}
	return err
}

// as encoding/json, the v1 wording of jsontext's errors
var syntaxErrorReplacer = strings.NewReplacer(
	"object name", "object key",
	"at start of value", "looking for beginning of value",
	"at start of string", "looking for beginning of object key string",
	"after object value", "after object key:value pair",
	"in number", "in numeric literal",
)

const _UNEXPECTED_END = "unexpected end of JSON input"

// More reports whether there is another value in the stream
func (d *Decoder) More() bool {
	k := d.dec.PeekKind()
	if k == 0 {

		// invalid input, rather than the end: report more, as before, so
		// that the next Decode returns the error
		b := bytes.TrimLeft(d.dec.UnreadBuffer(), " \t\r\n")
		return len(b) > 0 && b[0] != ']' && b[0] != '}'
	}
	return k != ']' && k != '}'
}

func (d *Decoder) InputOffset() int64 {
	return d.dec.InputOffset()
}

// Buffered returns a reader of the data remaining in the Decoder's buffer
func (d *Decoder) Buffered() io.Reader {
	return bytes.NewReader(d.dec.UnreadBuffer())
}

// Token returns the next JSON token, with numbers as in Decode
func (d *Decoder) Token() (Token, error) {
	tok, err := d.dec.ReadToken()
	if err != nil {
		return nil, readError(err)
	}
	switch k := tok.Kind(); k {
	case '{', '}', '[', ']':
		return Delim(k), nil
	case 'n':
		return nil, nil
	case 't', 'f':
		return tok.Bool(), nil
	case '"':
		return tok.String(), nil
	case '0':
		if d.useNumber {
			return Number(tok.String()), nil
		}
		return convertNumber(tok.String(), d.dec.InputOffset())
	}
	return nil, errors.ErrUnsupported
}
