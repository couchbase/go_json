# go_json

This package contains the Go source code from the Go standard library "encoding/json",
and some custom fixes, as follows:
- The Unmarshal() code by default unmarshals integers into int64 values, rather than float64.
- The scanner code includes a Validate() method, taken from the github.com/dustin/gojson repository.
