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
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"
)

// simple and fast decoder skipping the whole Reflect and UnmarshalJSON machinery
func SimpleUnmarshal(data []byte) (any, error) {
	scan := newScanner(data)
	defer freeScanner(scan)
	scan.reset()
	return unmarshaledValue(scan)
}

// as above but avoiding string copies - the input buffer must be guaranteed to be immutable
func SimpleUnmarshalImmutable(data []byte) (any, error) {
	scan := newScanner(data)
	defer freeScanner(scan)
	scan.reset()
	scan.setImmutable()
	return unmarshaledValue(scan)
}

// initial sizes of arrays and objects: an object of up to 8 fields fits in a
// single group of a Go map, with no table to allocate
const (
	_ARRAY_SIZE  = 4
	_OBJECT_SIZE = 8
)

// the nesting limit of encoding/json, which strict decoding keeps to
const _MAX_STRICT_DEPTH = 10000

var errTooDeep = &SyntaxError{"exceeded max depth", 0}

// this is just to mark that there is no current value
type unsetType int

const unsetVal = unsetType(iota)

func unmarshaledValue(scan *scanner) (any, error) {
	var keys keyCache
	var current any
	var err error

	level := 0

outer:
	for scan.offset < len(scan.data) {

		oldOffset := scan.offset
		c := scan.data[oldOffset]
		scan.offset++
		newOp := scan.step(scan, c)

		switch newOp {

		// this is a string, a number, true, false or null
		case scanBeginLiteral:
			switch c {

			// the string is consumed here
			case '"':
				bytes, err := nextLiteral(scan)
				if err != nil {
					return nil, err
				}

				// an object key goes straight to its object, with no boxing
				if n := len(scan.parseState); level > 0 && n > 0 && scan.parseState[n-1] == parseObjectKey {
					scan.frames[level-1].key = keys.keyString(bytes, scan.toString)
					current = nil
				} else {
					current = scan.toString(bytes)
				}

			// the value is presumed here, and checked with the next states
			case 't':
				current = true
			case 'f':
				current = false
			case 'n':
				current = nil

			// it's a number, consumed here
			default:
				current, err = nextNumber(scan, c)
				if err != nil {
					return nil, err
				}
			}
		case scanBeginArray:
			level++
			if level > _MAX_STRICT_DEPTH && scan.strict {
				return nil, errTooDeep
			}
			scan.frames = append(scan.frames, frame{array: make([]any, 0, _ARRAY_SIZE)})

			// current will never be returned set to unsetVal - the error will be caught earlier
			current = unsetVal
		case scanArrayValue:
			if level == 0 || scan.frames[level-1].object != nil {
				return nil, fmt.Errorf("Unexpected array value, not in array")
			}
			top := &scan.frames[level-1]
			top.array = append(top.array, current)
		case scanEndArray:

			// there's no scanArrayValue before scanEndArray
			if level == 0 || scan.frames[level-1].object != nil {
				return nil, fmt.Errorf("Unexpected array value, not in array")
			}
			top := &scan.frames[level-1]

			// handle empty arrays: only append the current value if it is not unset
			if current != unsetVal {
				top.array = append(top.array, current)
			}
			current = top.array
			level--
			scan.popFrame()
		case scanBeginObject:
			level++
			if level > _MAX_STRICT_DEPTH && scan.strict {
				return nil, errTooDeep
			}
			scan.frames = append(scan.frames, frame{object: make(map[string]any, _OBJECT_SIZE)})
			current = unsetVal
		case scanObjectKey:
			if level == 0 || scan.frames[level-1].object == nil {
				return nil, fmt.Errorf("Unexpected object key, not in object")
			}
		case scanObjectValue:
			if level == 0 || scan.frames[level-1].object == nil {
				return nil, fmt.Errorf("Unexpected object value, not in object")
			}
			top := &scan.frames[level-1]
			top.object[top.key] = current
		case scanEndObject:

			// there's no scanObjectValue before scanEndObject
			if level == 0 || scan.frames[level-1].object == nil {
				return nil, fmt.Errorf("Unexpected object value, not in object")
			}
			top := &scan.frames[level-1]

			// handle empty object: only add the current value if it is not unset
			if current != unsetVal {
				top.object[top.key] = current
			}
			current = top.object
			level--
			scan.popFrame()
		case scanSkipSpace:
		case scanContinue:
		case scanEnd:
			if !scan.checkTop {
				break outer
			}
		case scanError:
			return nil, scan.err
		default:
			return nil, fmt.Errorf("Found unhandled json op: %v", newOp)
		}
	}

	// what follows is in essence an unwound (* scanner) eof()
	if scan.err != nil {
		return nil, scan.err
	}

	// we got to the end with no error
	if scan.endTop {
		return current, nil
	}

	// we didn't get to a complete value, process left over steps
	scan.step(scan, ' ')

	// still good
	if scan.endTop {
		return current, nil
	}
	if scan.err == nil || len(scan.data) == scan.offset {
		return nil, &SyntaxError{_UNEXPECTED_END, int64(scan.offset)}
	}
	return nil, scan.err
}

// nextLiteral scans the data and grabs the next literal, in one pass
func nextLiteral(scan *scanner) ([]byte, error) {
	l := len(scan.data)

	// first try and see if we can pass the literal straight from our slice
	start := scan.offset
	for {
		if scan.offset >= l {
			return nil, &SyntaxError{_UNEXPECTED_END, int64(scan.offset)}
		}
		c := scan.data[scan.offset]

		// found the other side
		if c == '"' {
			scan.step = stateEndValue
			oldOffset := scan.offset
			scan.offset++
			return scan.data[start:oldOffset], nil
		}

		// no control characters
		if c < 0x20 {
			_ = scan.error(c, "in string literal")
			return nil, scan.err
		}
		if c == '\\' || c >= utf8.RuneSelf {
			break
		}
		scan.offset++
	}

	usableCap := max((scan.offset-start)*2, 64)
	literal := make([]byte, usableCap)
	copy(literal, scan.data[start:scan.offset])
	out := scan.offset - start

	// we always leave 8 bytes for a possible utf16
	usableCap -= 8
	for {
		if scan.offset >= l {
			return nil, &SyntaxError{_UNEXPECTED_END, int64(scan.offset)}
		}
		c := scan.data[scan.offset]

		// found the other side
		if c == '"' {
			scan.step = stateEndValue
			scan.offset++
			return literal[:out], nil
		}

		// no control characters
		if c < 0x20 {
			_ = scan.error(c, "in string literal")
			return nil, scan.err
		}

		// make more space
		if out >= usableCap {
			newCap := (usableCap + 8) * 2
			newLiteral := make([]byte, newCap)
			copy(newLiteral, literal[:out])
			literal = newLiteral
			usableCap = newCap - 8
		}

		// escape
		if c == '\\' {
			scan.offset++

			// the input must not end inside the escape sequence
			if scan.offset >= l {
				return nil, &SyntaxError{_UNEXPECTED_END, int64(scan.offset)}
			}
			c := scan.data[scan.offset]
			scan.offset++
			switch c {
			case '"', '\\', '/':
				literal[out] = c
			case '\'':
				if scan.strict {
					_ = scan.error(c, "in string escape code")
					return nil, scan.err
				}
				literal[out] = c
			case 'b':
				literal[out] = '\b'
			case 'f':
				literal[out] = '\f'
			case 'n':
				literal[out] = '\n'
			case 'r':
				literal[out] = '\r'
			case 't':
				literal[out] = '\t'
			case 'u':
				oldOffset := scan.offset - 2
				rr, size := getu4OrSurrogate(scan.data, oldOffset)
				if rr < 0 {
					_ = scan.error(c, "invalid unicode sequence")
					return nil, scan.err
				}
				scan.offset = oldOffset + size
				out += utf8.EncodeRune(literal[out:], rr)
				continue
			default:
				_ = scan.error(c, "invalid escaped character")
				return nil, scan.err
			}
			out++

			// ascii
		} else if c < utf8.RuneSelf {
			literal[out] = c
			out++
			scan.offset++

			// UTFs
		} else {
			rr, size := utf8.DecodeRune(scan.data[scan.offset:])
			scan.offset += size
			out += utf8.EncodeRune(literal[out:], rr)
		}
	}

	// we should never get here
	_ = scan.error(' ', "unxepected state")
	return nil, scan.err

}

const _MaxInt64Over10 = math.MaxInt64 / 10

// nextNumber scans the data and grabs the next number, in one pass
func nextNumber(scan *scanner, c byte) (any, error) {
	isNeg := c == '-'
	tot := int64(0)
	if !isNeg {
		tot = int64(c - '0')
	}

	// first byte has already been consumed
	start := scan.offset - 1
	numStart := start
	if isNeg {
		numStart++
	}

outer:
	for scan.offset < len(scan.data) {

		oldOffset := scan.offset
		c = scan.data[oldOffset]
		scan.offset++
		newOp := scan.step(scan, c)
		switch newOp {
		case scanContinue:
			if scan.useInts {
				if tot > 0 && tot < _MaxInt64Over10 {
					// if we know it'll be < MaxInt64
					// note we use '<' instead of '<=' since if it's == it's
					// not always < MaxInt64 (depending on next digit)
					tot = tot*10 + int64(c-'0')
				} else if oldOffset-numStart < 19 {
					// otherwise, make sure we don't go over MaxInt64
					src := string(scan.data[start:scan.offset])
					i, err := strconv.ParseInt(src, 10, 64)
					if err == nil && i >= math.MinInt64 && i <= math.MaxInt64 {
						tot = i
						isNeg = false // '-' sign alredy parsed
					} else {
						scan.useInts = false
					}
				} else {
					scan.useInts = false
				}
			}
		case scanError:
			return nil, scan.err
		default:
			scan.undo(newOp)
			break outer
		}
	}

	if scan.useInts {
		scan.useInts = false
		if isNeg {
			return -tot, nil
		} else if tot == math.MinInt64 && scan.strict {

			// as Unmarshal always has, rather than an int64
			return float64(tot), nil
		}
		return tot, nil
	}
	src := string(scan.data[start:scan.offset])

	f, err := strconv.ParseFloat(src, 64)
	if err != nil {
		return nil, &UnmarshalTypeError{Value: "number " + src, Type: float64Type, Offset: int64(scan.offset)}
	}
	return f, nil
}

// nextScanValue identifies the next whole value and returns it,
// preserving the scan, and moving it along to the rest of the document
// useful in single scan environments
// it does not need a data argument, and will not return rest
func nextScanValue(scan *scanner) ([]byte, error) {
	var saveScan scanner

	// avoid cost of scanner allocation
	// avoid needless stateEndTop error, since we are scanning mid scan
	// setScanner reuses, and clears, the frame stack shared with saveScan:
	// it must be empty, as the scan being saved is not unmarshalling
	saveScan = *scan
	scan = setScanner(scan, scan.data)
	scan.reset()
	scan.checkTop = false
	scan.offset = saveScan.offset

	// get to beginning of token
	if scan.offset < len(scan.data) {
		c := scan.data[scan.offset]
		if isSpace(c) {
			scan.offset++
		}
	}

	start := scan.offset
	for scan.offset < len(scan.data) {
		i := scan.offset
		c := scan.data[i]
		scan.offset++
		v := scan.step(scan, c)
		if v >= scanEndObject {
			switch v {
			case scanEndObject, scanEndArray:

				// since we are looking for a value within a scan, here we do not expect
				// the scan to end, and there will be something after the value
				// the next scan step therefore is stateEndValue
				if scan.step(scan, ' ') == scanEnd {
					saveScan.offset = scan.offset
					saveScan.step = stateEndValue
					*scan = saveScan
					return scan.data[start : i+1], nil
				}
			case scanError:
				*scan = saveScan
				return nil, scan.err
			case scanEnd:
				*scan = saveScan
				return scan.data[start:i], nil
			}
		}
	}
	if scan.eof() == scanError {
		*scan = saveScan
		return nil, scan.err
	}
	*scan = saveScan
	return scan.data[start:], nil
}

func nextUnmarshalledValue(scan *scanner) (any, error) {
	var saveScan scanner

	// avoid cost of scanner allocation
	// setScanner reuses, and clears, the frame stack shared with saveScan:
	// it must be empty, as the scan being saved is not unmarshalling
	saveScan = *scan

	scan = setScanner(scan, scan.data)
	scan.reset()
	scan.offset = saveScan.offset
	scan.toString = saveScan.toString

	// avoid needless stateEndTop error, since we are scanning mid scan
	scan.checkTop = false

	val, err := unmarshaledValue(scan)

	// on an error, the frames hold the values being built: clear them, rather
	// than keep them alive until the scanner is next set up
	clear(scan.frames)

	// unmarshalValue has gone one byte too far
	saveScan.offset = scan.offset - 1
	saveScan.step = stateEndValue
	*scan = saveScan
	return val, err
}
