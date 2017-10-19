package json

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type FindState struct {
	found map[string][]byte
	level int
	scan  *scanner
}

func arreq(a, b []string) bool {
	if len(a) == len(b) {
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	return false
}

func unescape(s string) string {
	n := strings.Count(s, "~")
	if n == 0 {
		return s
	}

	t := make([]byte, len(s)-n+1) // remove one char per ~
	w := 0
	start := 0
	for i := 0; i < n; i++ {
		j := start + strings.Index(s[start:], "~")
		w += copy(t[w:], s[start:j])
		if len(s) < j+2 {
			t[w] = '~'
			w++
			break
		}
		c := s[j+1]
		switch c {
		case '0':
			t[w] = '~'
			w++
		case '1':
			t[w] = '/'
			w++
		default:
			t[w] = '~'
			w++
			t[w] = c
			w++
		}
		start = j + 2
	}
	w += copy(t[w:], s[start:])
	return string(t[0:w])
}

func parsePointer(s string) []string {
	a := strings.Split(s[1:], "/")
	if !strings.Contains(s, "~") {
		return a
	}

	for i := range a {
		if strings.Contains(a[i], "~") {
			a[i] = unescape(a[i])
		}
	}
	return a
}

func escape(s string, out []rune) []rune {
	for _, c := range s {
		switch c {
		case '/':
			out = append(out, '~', '1')
		case '~':
			out = append(out, '~', '0')
		default:
			out = append(out, c)
		}
	}
	return out
}

func encodePointer(p []string) string {
	out := make([]rune, 0, 64)

	for _, s := range p {
		out = append(out, '/')
		out = escape(s, out)
	}
	return string(out)
}

func grokLiteral(b []byte) string {
	s, ok := unquoteBytes(b)
	if !ok {
		panic("could not grok literal " + string(b))
	}
	return string(s)
}

// FindDecode finds an object by JSONPointer path and then decode the
// result into a user-specified object.  Errors if a properly
// formatted JSON document can't be found at the given path.
func FindDecode(data []byte, path string, into interface{}) error {
	d, err := Find(data, path)
	if err != nil {
		return err
	}
	return Unmarshal(d, into)
}

// Find a section of raw JSON by specifying a JSONPointer.
func Find(data []byte, path string) ([]byte, error) {
	var lastLiteral string

	if path == "" {
		return data, nil
	}

	needle := parsePointer(path)

	scan := newScanner(data)
	scan.reset()

	current := make([]string, 0, 32)
	level := -1
	for scan.offset < len(scan.data) {

		oldOffset := scan.offset
		c := data[oldOffset]
		scan.offset++
		newOp := scan.step(scan, c)

		switch newOp {
		case scanBeginArray:
			level++
			current = append(current, "0")
		case scanObjectKey:
			current[len(current)-1] = lastLiteral
		case scanBeginLiteral:
			if level >= 0 && scan.parseState[level] == parseObjectKey {
				literal, err := nextLiteral(scan)
				if err != nil {
					return nil, err
				}
				lastLiteral = string(literal)
			}
		case scanArrayValue:
			n := mustParseInt(current[len(current)-1])
			current[len(current)-1] = strconv.Itoa(n + 1)
		case scanEndArray, scanEndObject:
			current = sliceToEnd(current)
			level--
		case scanBeginObject:
			level++
			current = append(current, "")
		case scanContinue, scanSkipSpace, scanObjectValue, scanEnd:
		case scanError:
			return nil, scan.err
		default:
			return nil, fmt.Errorf("found unhandled json op: %v", newOp)
		}

		if (newOp == scanBeginArray || newOp == scanArrayValue ||
			newOp == scanObjectKey) && arreq(needle, current) {
			otmp := scan.offset
			for isSpace(data[otmp]) {
				otmp++
			}
			if data[otmp] == ']' {
				// special case an array offset miss
				return nil, nil
			}
			return nextScanValue(scan)
		}
	}

	return nil, nil
}

// Find a first level field
func FirstFind(data []byte, field string) ([]byte, error) {
	var current string

	if field == "" {
		return data, nil
	}

	scan := newScanner(data)
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
				if current == field {
					return nextScanValue(scan)
				}
			}
		case scanBeginLiteral:
			if level == 1 && scan.parseState[0] == parseObjectKey {
				literal, err := nextLiteral(scan)
				if err != nil {
					return nil, err
				}
				current = string(literal)
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

// initialize a FindState
func NewFindState(data []byte) *FindState {
	rv := &FindState{
		found: make(map[string][]byte, 32),
		scan:  newScanner(data),
	}
	rv.scan.reset()
	return rv
}

// Find a first level field, maintaining a state for later reuse
func FirstFindWithState(state *FindState, field string) ([]byte, error) {
	var current string

	if state == nil {
		return nil, fmt.Errorf("FindState is uninitialized")
	}

	if field == "" {
		return state.scan.data, nil
	}

	found, ok := state.found[field]
	if ok {
		return found, nil
	}

	level := state.level
	for state.scan.offset < len(state.scan.data) {

		oldOffset := state.scan.offset
		c := state.scan.data[oldOffset]
		state.scan.offset++
		newOp := state.scan.step(state.scan, c)

		switch newOp {
		case scanBeginArray:
			level++
		case scanObjectKey:
			if level == 1 {
				val, err := nextScanValue(state.scan)
				if err != nil {
					return nil, err
				}
				state.found[current] = val
				if current == field {
					state.level = level
					return val, nil
				}
			}
		case scanBeginLiteral:
			if level == 1 && state.scan.parseState[0] == parseObjectKey {
				literal, err := nextLiteral(state.scan)
				if err != nil {
					return nil, err
				}
				current = string(literal)
			}
		case scanArrayValue:
		case scanEndArray, scanEndObject:
			level--
		case scanBeginObject:
			level++
		case scanContinue, scanSkipSpace, scanObjectValue, scanEnd:
		case scanError:
			return nil, state.scan.err
		default:
			return nil, fmt.Errorf("found unhandled json op: %v", newOp)
		}
	}

	return nil, nil
}

func sliceToEnd(s []string) []string {
	end := len(s) - 1
	if end >= 0 {
		s = s[:end]
	}
	return s

}

func mustParseInt(s string) int {
	n, err := strconv.Atoi(s)
	if err == nil {
		return n
	}
	panic(err)
}

// ListPointers lists all possible pointers from the given input.
func ListPointers(data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("Invalid JSON")
	}
	rv := []string{""}

	scan := newScanner(data)
	scan.reset()

	beganLiteral := 0
	var current []string
	for {
		if scan.offset >= len(data) {
			return rv, nil
		}
		oldOffset := scan.offset
		c := data[oldOffset]
		scan.offset++
		newOp := scan.step(scan, c)

		switch newOp {
		case scanBeginArray:
			current = append(current, "0")
		case scanObjectKey:
			current[len(current)-1] = grokLiteral(data[beganLiteral-1 : oldOffset])
		case scanBeginLiteral:
			beganLiteral = scan.offset
		case scanArrayValue:
			n := mustParseInt(current[len(current)-1])
			current[len(current)-1] = strconv.Itoa(n + 1)
		case scanEndArray, scanEndObject:
			current = sliceToEnd(current)
		case scanBeginObject:
			current = append(current, "")
		case scanError:
			return nil, fmt.Errorf("Error reading JSON object at offset %v", oldOffset)
		}

		if newOp == scanBeginArray || newOp == scanArrayValue ||
			newOp == scanObjectKey {
			rv = append(rv, encodePointer(current))
		}
	}
}

// FindMany finds several jsonpointers in one pass through the input.
func FindMany(data []byte, paths []string) (map[string][]byte, error) {
	tpaths := make([]string, 0, len(paths))
	m := map[string][]byte{}
	for _, p := range paths {
		if p == "" {
			m[p] = data
		} else {
			tpaths = append(tpaths, p)
		}
	}
	sort.Strings(tpaths)

	scan := newScanner(data)
	scan.reset()

	todo := len(tpaths)
	beganLiteral := 0
	matchedAt := 0
	var current []string
	for todo > 0 {
		if scan.offset >= len(data) {
			break
		}
		oldOffset := scan.offset
		c := data[oldOffset]
		scan.offset++
		newOp := scan.step(scan, c)

		switch newOp {
		case scanBeginArray:
			current = append(current, "0")
		case scanObjectKey:
			current[len(current)-1] = grokLiteral(data[beganLiteral-1 : oldOffset])
		case scanBeginLiteral:
			beganLiteral = scan.offset
		case scanArrayValue:
			n := mustParseInt(current[len(current)-1])
			current[len(current)-1] = strconv.Itoa(n + 1)
		case scanEndArray, scanEndObject:
			current = sliceToEnd(current)
		case scanBeginObject:
			current = append(current, "")
		}

		if newOp == scanBeginArray || newOp == scanArrayValue ||
			newOp == scanObjectKey {

			if matchedAt < len(current)-1 {
				continue
			}
			if matchedAt > len(current) {
				matchedAt = len(current)
			}

			currentStr := encodePointer(current)
			off := sort.SearchStrings(tpaths, currentStr)
			if off < len(tpaths) {
				// Check to see if the path we're
				// going down could even lead to a
				// possible match.
				if strings.HasPrefix(tpaths[off], currentStr) {
					matchedAt++
				}
				// And if it's not an exact match, keep parsing.
				if tpaths[off] != currentStr {
					continue
				}
			} else {
				// Fell of the end of the list, no possible match
				continue
			}

			// At this point, we have an exact match, so grab it.
			stmp := newScanner(data)
			stmp.offset = scan.offset
			val, _, err := nextValue(data, stmp)
			if err != nil {
				return m, err
			}
			m[currentStr] = val
			todo--
		}
	}

	return m, nil
}
