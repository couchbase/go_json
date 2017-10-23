# go_json

This package contains the Go source code from the Go standard library "encoding/json",
and some custom fixes, as follows:

* The principal focus of this work has been performance improvements, including:
    * avoiding state changes at every byte (for instance when skipping blanks or going through literals or strings).
* The Unmarshal() code by default unmarshals integers into int64 values, rather than float64.
* The scanner code includes a Validate() method, taken from the [github.com/dustin/gojson](http://github.com/dustin/gojson) repository.
* A partial implementation of jsonpointer, based on work found in [github.com/dustin/go-jsonpointer](http://github.com/dustin/go-jsonpointer) has been added, augmented as follows
    * this too optimized for performance as described above.
    * new FirstFind() routine has been added which finds any first level object elements.
    * new FirstFindWithState() routine has been added, which finds any first level object element, saving elelements found along the way, and restarting the scan from whence it left.
