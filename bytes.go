package json

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

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
	var sc scanner

	if path == "" {
		return data, nil
	}

	needle := parsePointer(path)

	scan := setScanner(&sc, data)
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
				res, err := nextLiteral(scan)
				if err != nil {
					return nil, err
				}
				lastLiteral = string(res)
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
	var sc scanner

	if len(data) == 0 {
		return nil, fmt.Errorf("Invalid JSON")
	}
	rv := []string{""}

	scan := setScanner(&sc, data)
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
	var sc1, sc2 scanner

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

	scan := setScanner(&sc1, data)
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
			stmp := setScanner(&sc2, data)
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
