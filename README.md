pkgrank
-------

Discover important Go package dependencies with graph centrality scores on the
package import DAG.

[![Go Report Card](https://goreportcard.com/badge/gojp/goreportcard)](https://goreportcard.com/report/gojp/goreportcard)

Example usage:
```
$ go get github.com/henrywallace/pkgrank
$ go get github.com/gohugoio/hugo
$ pkgrank github.com/gohugoio/hugo/...
0.024520 fmt
0.023570 strings
0.019538 github.com/gohugoio/hugo/deps
0.018792 sync
0.015960 github.com/gohugoio/hugo/tpl/internal
0.015799 github.com/spf13/cast
0.014199 bytes
0.014145 io
0.013702 path/filepath
0.012003 os
0.011667 github.com/gohugoio/hugo/helpers
0.011643 encoding/json
0.011520 errors
0.011435 reflect
0.010874 github.com/spf13/afero
0.010730 github.com/gohugoio/hugo/config
0.010219 time
```

Use `--prefix` to filter package imports with the given prefix, or `--pkg` to
iterate over imports by package, instead of by file.

Current implementation is naive by calling out to `go list` subprocesses. And
currently only pagerank centrality is implemented. Nodes are packages, and
edges are directed graph of A >- imports -> B. Edge weights are the number of
times that package A imports package B, which may be multiple if files are
iterated over instead of packages with `--pkg`.

Dual-licensed under MIT or the [UNLICENSE](http://unlicense.org).
