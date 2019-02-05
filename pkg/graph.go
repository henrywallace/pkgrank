package pkg

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/network"
	"gonum.org/v1/gonum/graph/simple"
)

// https://github.com/golang/go/blob/master/src/cmd/go/internal/load/pkg.go
// https://github.com/kisielk/godepgraph/blob/master/main.go
// https://en.wikipedia.org/wiki/Centrality#PageRank_centrality
// https://github.com/golang/go/wiki/Modules#quick-start
// https://dave.cheney.net/2014/09/14/go-list-your-swiss-army-knife

// ListPackages ...
func ListPackages(root string) ([]string, error) {
	outBytes, err := exec.Command("go", "list", root).CombinedOutput()
	if err != nil {
		return nil, err
	}
	out := strings.TrimSpace(string(outBytes))
	if strings.HasPrefix(out, "go: warning: ") && strings.HasSuffix(out, "matched no packages") {
		return nil, errors.New(out)
	}
	return strings.Split(out, "\n"), nil
}

// ListGoFiles ...
func ListGoFiles(pkg string) ([]string, error) {
	args := []string{"list", "-f", `{{ .Dir }}`, pkg}
	outBytes, err := exec.Command("go", args...).CombinedOutput()
	if err != nil {
		return nil, errors.Wrap(err, string(outBytes))
	}
	dir := strings.TrimSpace(string(outBytes))

	args = []string{"list", "-f", `{{ join .GoFiles "\n" }}`, pkg}
	outBytes, err = exec.Command("go", args...).CombinedOutput()
	if err != nil {
		return nil, errors.Wrap(err, string(outBytes))
	}
	basenames := strings.Split(string(outBytes), "\n")

	var paths []string
	for _, basename := range basenames {
		paths = append(paths, filepath.Join(dir, basename))
	}
	return paths, nil
}

// ListImports lists all unique packges that the given package imports, not
// including test files.
func ListImports(pkgOrFile string, prefix string) ([]string, error) {
	args := []string{"list", "-f", `{{ join .Imports "\n" }}`, pkgOrFile}
	out, err := exec.Command("go", args...).CombinedOutput()
	if err != nil {
		return nil, errors.Wrap(err, string(out))
	}
	allImports := strings.Split(string(out), "\n")
	var filtered []string
	for _, imp := range allImports {
		if imp == "" {
			continue
		}
		if strings.Contains(imp, "vendor/") {
			continue
		}
		if !strings.HasPrefix(imp, prefix) {
			continue
		}
		filtered = append(filtered, imp)
	}
	return filtered, nil
}

// BuildGraph ...
func BuildGraph(pkgs []string, prefix string, iterPkg bool) (*ImportGraph, error) {
	g := NewImportGraph()
	wg := new(sync.WaitGroup)
	sem := make(chan struct{}, 32)
	mu := new(sync.Mutex)
	for _, pkg := range pkgs {
		wg.Add(1)
		go func(pkg string) {
			defer func() {
				wg.Done()
				<-sem
			}()
			sem <- struct{}{}
			var targets []string
			if iterPkg {
				targets = []string{pkg}
			} else {
				files, err := ListGoFiles(pkg)
				if err != nil {
					fmt.Println("ERR", err)
					return
				}
				targets = files
			}
			var allImports []string
			for _, pkgOrFile := range targets {
				imps, err := ListImports(pkgOrFile, prefix)
				if err != nil {
					fmt.Println("ERR", err)
					return
				}
				allImports = append(allImports, imps...)
			}
			mu.Lock()
			for _, imp := range allImports {
				g.UpdateEdge(pkg, imp)
			}
			mu.Unlock()
		}(pkg)
	}
	wg.Wait()
	return g, nil
}

// ImportGraph ...
type ImportGraph struct {
	g          *simple.WeightedDirectedGraph
	idToImport map[int64]string
	importToID map[string]int64
}

// NewImportGraph ...
func NewImportGraph() *ImportGraph {
	return &ImportGraph{
		g:          simple.NewWeightedDirectedGraph(0, 0),
		idToImport: make(map[int64]string),
		importToID: make(map[string]int64),
	}
}

// Len returns the number of nodes in the graph.
func (g *ImportGraph) Len() int {
	return g.g.Nodes().Len()
}

// CentralityMeasure is a method of measuring the centrality of nodes.
type CentralityMeasure string

// Available centrality measures.
const (
	InvalidCentrality  CentralityMeasure = "invalid"
	PageRankCentrality                   = "pagerank"
)

// NewCentralityMeasure returns a new CentralityMeasure from the given raw
// string. An error is returned, if no such
func NewCentralityMeasure(s string) (CentralityMeasure, error) {
	switch s {
	case "pagerank":
		return PageRankCentrality, nil
	default:
		return InvalidCentrality, errors.Errorf("unsupported centrality measure: %s", s)
	}
}

// Centrality returns the a sorted slice of the most important packages in an
// import graph, with the most important listed first. A corresponding slice of
// importances is also returned.
func (g *ImportGraph) Centrality() ([]string, []float64) {
	if g.Len() == 0 {
		return nil, nil
	}
	centrality := network.PageRank(g.g, 0.85, 0.0001)
	type sortable struct {
		imp   string
		score float64
	}
	var sorted []sortable
	for id, score := range centrality {
		sorted = append(sorted, sortable{
			imp:   g.idToImport[id],
			score: score,
		})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})
	imps := make([]string, 0, len(centrality))
	scores := make([]float64, 0, len(centrality))
	for _, s := range sorted {
		imps = append(imps, s.imp)
		scores = append(scores, s.score)
	}
	return imps, scores
}

// UpdateEdge increases the weight on a directed edge between two imports in
// the graph, or creates a new one with weight 1.0 if one already doesn't
// exist. If nodes coressponding to the imports don't already exist, then they
// are created.
func (g *ImportGraph) UpdateEdge(imp1, imp2 string) {
	n1, n2 := g.AddNode(imp1), g.AddNode(imp2)
	we := g.g.WeightedEdge(n1.ID(), n2.ID())
	if we == nil {
		we = g.g.NewWeightedEdge(n1, n2, 1.0)
	} else {
		// Note that this case won't occur if we only loop over the
		// unique set of package imports, since imp1 is listed
		// uniquely. But it can occur if we iterate over imports
		// duplicately such as by file, or additionally including test
		// imports.
		we = g.g.NewWeightedEdge(n1, n2, we.Weight()+1)
	}
	g.g.SetWeightedEdge(we)

}

// AddNode idempotently returns a node representing the given import in the
// graph. If the import already has a node in the graph, then that existing
// node is returned. Otherwise, a new node is added and returned.
func (g *ImportGraph) AddNode(imp string) graph.Node {
	if id, ok := g.importToID[imp]; ok {
		return g.g.Node(id)
	}
	n := g.g.NewNode()
	g.g.AddNode(n)
	g.importToID[imp] = n.ID()
	g.idToImport[n.ID()] = imp
	return n
}
