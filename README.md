# go_json

This package started as a fork of the standard golang package "encoding/json",
with some custom fixes, as follows:

* The principal focus of this work has been performance improvements, including:
    * avoiding state changes at every byte (for instance when skipping blanks or going through literals or strings).
* The Unmarshal() code by default unmarshals integers into int64 values, rather than float64.
* The scanner code includes a Validate() method, taken from the [github.com/dustin/gojson](http://github.com/dustin/gojson) repository.

It then had a partial implementation of jsonpointer added to it, based on work found in [github.com/dustin/go-jsonpointer](http://github.com/dustin/go-jsonpointer), augmented as follows:

* this too optimized for performance as described above.

These improvements proved insufficient to address the N1QL language json scanning necessities, so while the whole package has been lelft for backwards compatibility, the following
has been implemented.

* Fast json field and array element scanning routines
    * FindKey(), which finds an object field in a single pass
    * (* KeyState).FindKey(), which does the same, but saving a state. This allows to cache fields alredy found, and restarting the scan from whence it left
    * FindIndex(), which finds an array element  in a single pass
    * (* IndexState).FindIndex(), which does the same, but saving a state. This allows to cache elements alredy found, and restarting the scan from whence it left
    * (* ScanState).ScanKeys(), which scans an object in a single pass, using a state. Each call returns the next field
    * (* ScanState).NextValue(), which returns a []byte representation of the value associated with the last field
    * (* ScanState).NextUnmarshaledValue(), which does the same, but unmarshals the value, again in a single pass.

* A simple Unmarshal routine (aptly names SimpleUnmarshal()) which unmarshals in a single pass a document into an interface{}, bypassing the UnmarshalJSON() and Reflect machinery:
this is useful to quickly unmarshal a document when no specific structure is expected.

The improved jsonpointer yields a 33% throughput improvement over the original code:

    BenchmarkAll-12                            49492             24272 ns/op

vs

    BenchmarkAll-12                            75120             16043 ns/op

The FindKey methods add a further 30% on stateless scans, while stateful scans are orders of magnitude faster on scan reuse, and 75% faster on a single scan:

    BenchmarkFindKey-12                       113839             10452 ns/op
    BenchmarkStateFindKey-12                 5484799               191 ns/op
    BenchmarkStateFindKeyRepeat-12            284469              4388 ns/op

The simple unmarshaler yields a 15% throughput improvement for small jsons, and 25% for large:

    BenchmarkAnonymousUnmarshal-12                40          28648282 ns/op          67.73 MB/s
    BenchmarkAnonymousUnmarshalBig-12             19          58053591 ns/op          34.36 MB/s

vs

    BenchmarkSimpleUnmarshal-12                   44          24966516 ns/op          77.72 MB/s
    BenchmarkSimpleUnmarshalBig-12                25          46440744 ns/op          42.95 MB/s

Traversing an object with ScanKeys() and NextUnmarshaledValue() offers a similar improvement over the simplest combination of
decoder.Token(), More() and Decode() (not to mention that the code is much more readable):

    BenchmarkDecodeMoreCode-12                    38          31210210 ns/op          62.17 MB/s
    BenchmarkDecodeMoreBig-12                     18          61598047 ns/op          32.38 MB/s

vs

    BenchmarkScanKeysCode-12                      44          24927408 ns/op          77.84 MB/s
    BenchmarkScanKeysBig-12                       25          46518892 ns/op          42.87 MB/s
