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
	"reflect"
	"strconv"
	"unicode/utf8"
)

// simple and fast decoder skipping the whole Reflect and UnmarshalJSON machinery
func SimpleUnmarshal(data []byte) (interface{}, error) {
	var scan scanner

	setScanner(&scan, data)
	scan.reset()
	return unmarshaledValue(&scan)
}

// this is just to mark that there is no current value
type unsetType int

const unsetVal = unsetType(iota)

func unmarshaledValue(scan *scanner) (interface{}, error) {
	var values []interface{}
	var keys []string
	var current interface{}
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
				current = string(bytes)

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
			values = append(values, make([]interface{}, 0, 10))

			// current will never be returned set to unsetVal - the error will be caught earlier
			current = unsetVal
		case scanArrayValue:
			if level == 0 {
				return nil, fmt.Errorf("Unexpected array value, not in array")
			}
			top, ok := values[level-1].([]interface{})
			if !ok {
				return nil, fmt.Errorf("Unexpected array value, not in array")
			}
			top = append(top, current)
			values[level-1] = top
		case scanEndArray:

			// there's no scanArrayValue before scanEndArray
			if level == 0 {
				return nil, fmt.Errorf("Unexpected array value, not in array")
			}
			top, ok := values[level-1].([]interface{})
			if !ok {
				return nil, fmt.Errorf("Unexpected array value, not in array")
			}

			// handle empty arrays: only append the current value if it is not unset
			if current != unsetVal {
				top = append(top, current)
			}
			current = top
			level--
			values = values[:level]
		case scanBeginObject:
			level++
			values = append(values, make(map[string]interface{}, 10))
			current = unsetVal
			keys = append(keys, "")
		case scanObjectKey:
			if len(keys) == 0 {
				return nil, fmt.Errorf("Unexpected object key, not in object")
			}
			key, ok := current.(string)
			if !ok {
				return nil, fmt.Errorf("Object key is not a string %v", current)
			}
			keys[len(keys)-1] = key
			current = nil
		case scanObjectValue:
			if level == 0 {
				return nil, fmt.Errorf("Unexpected object value, not in object")
			}
			top, ok := values[level-1].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("Unexpected object value, not in object")
			}
			key := keys[len(keys)-1]
			top[key] = current
		case scanEndObject:

			// there's no scanObjectValue before scanEndObject
			if level == 0 {
				return nil, fmt.Errorf("Unexpected object value, not in object")
			}
			top, ok := values[level-1].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("Unexpected object value, not in object")
			}

			// handle empty object: only add the current value if it is not unset
			if current != unsetVal {
				key := keys[len(keys)-1]
				top[key] = current
			}
			current = top
			level--
			values = values[:level]
			keys = keys[:len(keys)-1]
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
		return nil, &SyntaxError{"unexpected end of JSON input", int64(scan.offset)}
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
			return nil, &SyntaxError{"unexpected end of JSON input", int64(scan.offset)}
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

	usableCap := (scan.offset - start) * 2
	if usableCap < 64 {
		usableCap = 64
	}
	literal := make([]byte, usableCap)
	copy(literal, scan.data[start:scan.offset])
	out := scan.offset - start

	// we always leave 8 bytes for a possible utf16
	usableCap -= 8
	for {
		if scan.offset >= l {
			return nil, &SyntaxError{"unexpected end of JSON input", int64(scan.offset)}
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
			c := scan.data[scan.offset]
			scan.offset++
			switch c {
			case '"', '\\', '/', '\'':
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

// nextNumber scans the data and grabs the next number, in one pass
func nextNumber(scan *scanner, c byte) (interface{}, error) {
	isNeg := c == '-'
	tot := int64(0)
	if !isNeg {
		tot = int64(c - '0')
	}

	// first byte has already been consumed
	start := scan.offset - 1

outer:
	for scan.offset < len(scan.data) {

		oldOffset := scan.offset
		c = scan.data[oldOffset]
		scan.offset++
		newOp := scan.step(scan, c)
		switch newOp {
		case scanContinue:
			if scan.useInts {

				// accumulate the current int64 up to 19 digits
				if oldOffset-start < 19 {
					tot = tot*10 + int64(c-'0')
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
		} else {
			return tot, nil
		}
	}
	src := string(scan.data[start:scan.offset])

	f, err := strconv.ParseFloat(src, 64)
	if err != nil {
		return nil, &UnmarshalTypeError{"number " + src, reflect.TypeOf(0.0), int64(scan.offset)}
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

func nextUnmarshalledValue(scan *scanner) (interface{}, error) {
	var saveScan scanner

	// avoid cost of scanner allocation
	saveScan = *scan

	scan = setScanner(scan, scan.data)
	scan.reset()
	scan.offset = saveScan.offset

	// avoid needless stateEndTop error, since we are scanning mid scan
	scan.checkTop = false

	val, err := unmarshaledValue(scan)

	// unmarshalValue has gone one byte too far
	saveScan.offset = scan.offset - 1
	saveScan.step = stateEndValue
	*scan = saveScan
	return val, err
}
