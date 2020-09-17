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
)

type KeyState struct {
	found map[string][]byte
	level int
	scan  scanner
}

type ScanState struct {
	level int
	step  int
	scan  scanner
}

type IndexState struct {
	found    [][]byte
	level    int
	position int
	scan     scanner
}

// Find a first level field
func FindKey(data []byte, field string) ([]byte, error) {
	var current []byte
	var sc scanner

	if field == "" {
		return data, nil
	}

	scan := setScanner(&sc, data)
	scan.reset()
	level := 0

	for scan.offset < len(scan.data) {

		oldOffset := scan.offset
		c := data[oldOffset]
		scan.offset++
		newOp := scan.step(scan, c)

		switch newOp {
		case scanBeginArray:
			level++
		case scanObjectKey:
			if level == 1 {
				if string(current) == field {
					return nextScanValue(scan)
				}
			}
		case scanBeginLiteral:
			if level == 1 && scan.parseState[0] == parseObjectKey {
				res, err := nextLiteral(scan)
				if err != nil {
					return nil, err
				}
				current = res
			}
		case scanArrayValue:
		case scanEndArray, scanEndObject:
			level--
		case scanBeginObject:
			level++
		case scanContinue, scanSkipSpace, scanObjectValue, scanEnd:
		case scanError:
			return nil, scan.err
		default:
			return nil, fmt.Errorf("found unhandled json op: %v", newOp)
		}
	}

	return nil, nil
}

// initialize a KeyState
func SetKeyState(state *KeyState, data []byte) {
	if state.scan.data == nil {
		*state = KeyState{}
		state.found = make(map[string][]byte, 32)
		setScanner(&state.scan, data)
		state.scan.reset()
	}
}

// release state
func (state *KeyState) Release() {
	state.scan = scanner{}
	state.found = nil
}

// Find a first level field, maintaining a state for later reuse
func (state *KeyState) FindKey(field string) ([]byte, error) {
	var current []byte

	if field == "" {
		return state.scan.data, nil
	}

	found, ok := state.found[field]
	if ok {
		return found, nil
	}

	level := state.level
	scan := &state.scan
	for scan.offset < len(scan.data) {

		oldOffset := scan.offset
		c := scan.data[oldOffset]
		scan.offset++
		newOp := state.scan.step(scan, c)

		switch newOp {
		case scanBeginArray:
			level++
		case scanObjectKey:
			if level == 1 {
				val, err := nextScanValue(scan)
				if err != nil {
					return nil, err
				}
				state.found[string(current)] = val
				if string(current) == field {
					state.level = level
					return val, nil
				}
			}
		case scanBeginLiteral:
			if level == 1 && scan.parseState[0] == parseObjectKey {
				var err error

				res, err := nextLiteral(scan)
				if err != nil {
					return nil, err
				}
				current = res
			}
		case scanArrayValue:
		case scanEndArray, scanEndObject:
			level--
		case scanBeginObject:
			level++
		case scanContinue, scanSkipSpace, scanObjectValue, scanEnd:
		case scanError:
			return nil, scan.err
		default:
			return nil, fmt.Errorf("found unhandled json op: %v", newOp)
		}
	}

	state.level = level
	return nil, nil
}

func (state *KeyState) EOS() bool {
	return state.scan.offset >= len(state.scan.data)
}

// initialize a ScanState
func SetScanState(state *ScanState, data []byte) {
	if state.scan.data == nil {
		*state = ScanState{}
		setScanner(&state.scan, data)
		state.scan.reset()
	}
}

// release state
func (state *ScanState) Release() {
	state.scan = scanner{}
}

// retrieves the next key in an object
func (state *ScanState) ScanKeys() ([]byte, error) {
	var current []byte

	level := state.level
	scan := &state.scan
	for scan.offset < len(scan.data) {

		oldOffset := scan.offset
		c := scan.data[oldOffset]
		scan.offset++
		state.step = scan.step(scan, c)

		switch state.step {
		case scanBeginArray:
			level++
		case scanObjectKey:
			if level == 1 {
				state.level = level
				return current, nil
			}
		case scanBeginLiteral:
			if level == 1 && scan.parseState[0] == parseObjectKey {
				var err error

				res, err := nextLiteral(scan)
				if err != nil {
					return nil, err
				}
				current = res
			}
		case scanArrayValue:
		case scanEndArray, scanEndObject:
			level--
		case scanBeginObject:
			level++
		case scanContinue, scanSkipSpace, scanObjectValue, scanEnd:
		case scanError:
			return nil, scan.err
		default:
			return nil, fmt.Errorf("found unhandled json op: %v", state.step)
		}
	}

	state.level = level
	return nil, nil
}

// retrieves the value associated with current key
func (state *ScanState) NextValue() ([]byte, error) {
	if state.step == scanObjectKey && state.level == 1 {
		val, err := nextScanValue(&state.scan)
		state.step = scanObjectValue
		return val, err
	}
	return nil, fmt.Errorf("Not after object key")
}

// retrieves and unmarshals the value associated with current key
func (state *ScanState) NextUnmarshaledValue() (interface{}, error) {
	if state.step == scanObjectKey && state.level == 1 {
		val, err := nextUnmarshalledValue(&state.scan)
		state.step = scanObjectValue
		return val, err
	}
	return nil, fmt.Errorf("Not after object key")
}

func (state *ScanState) EOS() bool {
	return state.scan.offset >= len(state.scan.data)
}

// Find an array element
func FindIndex(data []byte, index int) ([]byte, error) {
	var sc scanner

	if index < 0 {
		return nil, fmt.Errorf("invalid array index")
	}

	scan := setScanner(&sc, data)
	scan.reset()
	level := 0
	position := 0

	for scan.offset < len(scan.data) {
		oldOffset := scan.offset
		c := data[oldOffset]
		scan.offset++
		newOp := scan.step(scan, c)

		switch newOp {
		case scanBeginArray:
			level++
			if level == 1 {
				if index == 0 {
					return nextScanValue(scan)
				}
			}
		case scanObjectKey:
		case scanBeginLiteral:
		case scanArrayValue:
			if level == 1 {
				position++
				if index == position {
					return nextScanValue(scan)
				}
			}
		case scanEndArray, scanEndObject:
			level--
		case scanBeginObject:
			level++
		case scanContinue, scanSkipSpace, scanObjectValue, scanEnd:
		case scanError:
			return nil, scan.err
		default:
			return nil, fmt.Errorf("found unhandled json op: %v", newOp)
		}
	}

	return nil, nil
}

// initialize an IndexState
func SetIndexState(state *IndexState, data []byte) {
	if state.scan.data == nil {
		*state = IndexState{}
		state.found = make([][]byte, 0, 32)
		setScanner(&state.scan, data)
		state.position = -1
		state.scan.reset()
	}
}

// release state
func (state *IndexState) Release() {
	state.scan = scanner{}
	state.found = nil
}

// Find an array element, maintaining a state for later reuse
func (state *IndexState) FindIndex(index int) ([]byte, error) {
	if index < 0 {
		return nil, fmt.Errorf("invalid array index")
	}

	// been here already
	if index <= state.position {
		return state.found[index], nil
	}

	// have already hit of array
	if state.position < 0 {
		state.position = 0
	} else if state.level == 0 {
		return nil, nil
	}

	scan := &state.scan
	for scan.offset < len(scan.data) {

		oldOffset := scan.offset
		c := scan.data[oldOffset]
		scan.offset++
		newOp := scan.step(scan, c)

		switch newOp {
		case scanBeginArray:
			state.level++
			if state.level == 1 {
				val, err := nextScanValue(scan)
				if err != nil {
					return nil, err
				}
				state.found = append(state.found, val)
				if index == 0 {
					return val, err
				}
			}
		case scanObjectKey:
		case scanBeginLiteral:
		case scanArrayValue:
			if state.level == 1 {
				val, err := nextScanValue(scan)
				if err != nil {
					return nil, err
				}
				state.found = append(state.found, val)
				state.position++
				if index == state.position {
					return val, err
				}
			}
		case scanEndArray, scanEndObject:
			state.level--
		case scanBeginObject:
			state.level++
		case scanContinue, scanSkipSpace, scanObjectValue, scanEnd:
		case scanError:
			return nil, scan.err
		default:
			return nil, fmt.Errorf("found unhandled json op: %v", newOp)
		}
	}

	return nil, nil
}

func (state *IndexState) EOS() bool {
	return state.scan.offset >= len(state.scan.data)
}
