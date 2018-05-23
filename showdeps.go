package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kisielk/gotool"
	"github.com/rogpeppe/godeps/build"
)

var (
	noTestDeps = flag.Bool("T", false, "exclude test dependencies")
	all        = flag.Bool("a", false, "show all dependencies recursively (only test dependencies from the root packages are shown); when used with -why, show all intermediate packages")
	std        = flag.Bool("stdlib", false, "show stdlib dependencies")
	from       = flag.Bool("from", false, "show which dependencies are introduced by which packages")
	why        = flag.String("why", "", "show only packages which import directly or indirectly the specified package (implies -a and -from)")
	files      = flag.Bool("f", false, "list Go source files instead of packages (overrides -from and -why)")
	maxChain   = flag.Int("n", 1, "max number of dependencies to print with -why (0 implies unlimited)")
)

var whyMatch func(string) bool

var helpMessage = `
usage: showdeps [flags] [pkg....]

showdeps prints Go package dependencies of the named packages, specified
as in the Go command (for instance ... wildcards work), one per line.
If no packages are given, it uses the package in the current directory.

Note that testing dependencies are only considered if they are
in the packages specified on the command line. That is testing
dependencies are not considered transitively.

By default it prints direct dependencies of the packages (and their tests)
only, but the -a flag can be used to print all reachable dependencies.

If the -from flag is specified, the package path on each line is followed
by the paths of all the packages that depend on it.

The -why flag finds out why a given dependency is present.  By default,
it prints one arbitrary dependency chain for each package specified on
the command line, showing why that package depends on the -why argument
(which may also be a Go-command-style ... wildcard pattern).  If the
package does not depend on the -why argument, it will not be printed. If
the -a flag is specified, all packages in in any dependency chain will
printed in -from style. The -n flag can be used to print up to a given
maximum number of arbitrary dependency chains - every dependency chain
printed will have at least one different package in it.

If the -f flag is provided, instead of packages, showdeps will print all
the Go source files in the package. It also includes the source of the
packages specified directly on the command line, including their test
files unless the -T flag is provided.

`[1:]

var cwd string

var (
	buildContext = func() build.Context {
		ctx := build.Default
		ctx.MatchTag = func(tag string, neg bool) bool {
			if build.KnownOS(tag) || build.KnownArch(tag) {
				return true
			}
			// Fall back to default settings for all other tags.
			return ctx.DefaultMatchTag(tag) != neg
		}
		return ctx
	}()
)

func main() {
	flag.Usage = func() {
		os.Stderr.WriteString(helpMessage)
		flag.PrintDefaults()
		os.Exit(2)
	}
	flag.Parse()
	pkgs := flag.Args()
	if len(pkgs) == 0 {
		pkgs = []string{"."}
	}
	if d, err := os.Getwd(); err != nil {
		log.Fatalf("cannot get working directory: %v", err)
	} else {
		cwd = d
	}
	recur := false
	showAllWhy := false
	if *why != "" {
		recur = true
		if *all {
			*from = true
			showAllWhy = true
		}
		if isStdlib(*why) {
			*std = true
		}
		whyMatch = matchPattern(*why)
	} else {
		recur = *all
	}

	pkgs = gotool.ImportPaths(pkgs)
	rootPkgs := make(map[string]bool)
	for _, pkg := range pkgs {
		p, err := buildContext.Import(pkg, cwd, build.FindOnly)
		if err != nil {
			log.Fatalf("cannot find %q: %v", pkg, err)
		}
		rootPkgs[p.ImportPath] = true
	}
	allPkgs := make(map[string][]string)
	for pkg := range rootPkgs {
		if err := findImports(pkg, cwd, recur, allPkgs, rootPkgs); err != nil {
			log.Fatalf("cannot find imports from %q: %v", pkg, err)
		}
	}
	if !*files {
		// Delete packages specified directly on the command line.
		for pkg := range rootPkgs {
			delete(allPkgs, pkg)
		}
		if whyMatch != nil {
			// Delete all packages that don't directly or indirectly import *why.
			marked := make(map[string]bool)
			for pkg := range allPkgs {
				if whyMatch(pkg) {
					markImporters(pkg, allPkgs, marked)
				}
			}
			for pkg := range allPkgs {
				if !marked[pkg] {
					delete(allPkgs, pkg)
				}
			}
		}
	}

	result := make([]string, 0, len(allPkgs))
	for name, from := range allPkgs {
		result = append(result, name)
		sort.Strings(from)
	}
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	sort.Strings(result)
	if *why != "" && !showAllWhy {
		showNReasonsWhy(w, allPkgs, rootPkgs)
		return
	}
	for _, r := range result {
		switch {
		case *files:
			pkg, _ := buildContext.Import(r, cwd, 0)
			showFiles(w, pkg, pkg.GoFiles)
			showFiles(w, pkg, pkg.CgoFiles)
			if rootPkgs[pkg.ImportPath] && !*noTestDeps {
				// It's a package specified directly on the command line.
				// Show its test files too.
				showFiles(w, pkg, pkg.TestGoFiles)
				showFiles(w, pkg, pkg.XTestGoFiles)
			}
		case *from:
			from := allPkgs[r]
			sort.Strings(from)
			from = uniq(from)
			fmt.Fprintf(w, "%s %s\n", r, strings.Join(from, " "))
		default:
			fmt.Fprintln(w, r)
		}
	}
}

// showNReasonsWhy shows up to maxChain lines for each package in the initial packages, each line showing
// one dependency path from that package to a package matched by *why.
func showNReasonsWhy(w io.Writer, allPkgs map[string][]string, rootPkgs map[string]bool) {
	chains := make(map[string][][]string)
	for pkg := range allPkgs {
		if !whyMatch(pkg) {
			continue
		}
		iterDepChains(pkg, rootPkgs, allPkgs, func(chain []string) {
			pkg := chain[len(chain)-1]
			if *maxChain > 0 && len(chains[pkg]) >= *maxChain {
				return
			}
			chain1 := make([]string, len(chain))
			for i, p := range chain {
				chain1[len(chain)-i-1] = p
			}
			chains[pkg] = append(chains[pkg], chain1)
		})
	}
	whyRoots := make([]string, 0, len(chains))
	for pkg := range chains {
		whyRoots = append(whyRoots, pkg)
	}
	sort.Strings(whyRoots)
	for _, pkg := range whyRoots {
		for _, chain := range chains[pkg] {
			fmt.Fprintf(w, "%s\n", strings.Join(chain, " "))
		}
	}
	return
}

// iterDepChains calls f with dependency chains to the given leaf package. The function is called with
// leaf first and its importers sequentially after it.
// It does not call f with *all* dependency chains, just the first chain that
// it encounters that leads to a given package.
func iterDepChains(leaf string, rootPkgs map[string]bool, allPkgs map[string][]string, f func(chain []string)) {
	chain := make([]string, 1, len(allPkgs))
	chain[0] = leaf
	iterDepChains1(chain, make(map[string]bool), rootPkgs, allPkgs, f)
}

func iterDepChains1(chain []string, visited map[string]bool, rootPkgs map[string]bool, allPkgs map[string][]string, f func(chain []string)) {
	pkg := chain[len(chain)-1]
	if rootPkgs[pkg] {
		f(chain)
		return
	}
	if visited[pkg] {
		return
	}
	visited[pkg] = true
	for _, importer := range allPkgs[pkg] {
		iterDepChains1(append(chain, importer), visited, rootPkgs, allPkgs, f)
	}
}

func showFiles(w io.Writer, pkg *build.Package, fs []string) {
	for _, f := range fs {
		fmt.Fprintln(w, filepath.Join(pkg.Dir, f))
	}
}

func uniq(ss []string) []string {
	j := 0
	prev := ""
	for _, s := range ss {
		if s != prev {
			ss[j] = s
			j++
			prev = s
		}
	}
	return ss[0:j]
}

// markImporters sets a marked entry to true for every entry in allPkgs
// that is imported by pkg, including pkg itself.
func markImporters(pkg string, allPkgs map[string][]string, marked map[string]bool) {
	if marked[pkg] {
		return
	}
	marked[pkg] = true // prevent infinite recursion
	for _, imp := range allPkgs[pkg] {
		markImporters(imp, allPkgs, marked)
	}
}

func isStdlib(pkg string) bool {
	return !strings.Contains(strings.SplitN(pkg, "/", 2)[0], ".")
}

// findImports recursively adds all imported packages by the given
// package (packageName) to the allPkgs map.
func findImports(packageName, dir string, recur bool, allPkgs map[string][]string, rootPkgs map[string]bool) error {
	if packageName == "C" {
		return nil
	}
	pkg, err := buildContext.Import(packageName, dir, 0)
	if err != nil {
		return fmt.Errorf("cannot find %q: %v", packageName, err)
	}
	allPkgs[pkg.ImportPath] = allPkgs[pkg.ImportPath] // ensure the package has an entry.
	for name := range imports(pkg, rootPkgs[pkg.ImportPath]) {
		if !*std && isStdlib(name) {
			continue
		}
		_, alreadyDone := allPkgs[name]
		allPkgs[name] = append(allPkgs[name], pkg.ImportPath)
		if recur && !alreadyDone {
			if err := findImports(name, pkg.Dir, recur, allPkgs, rootPkgs); err != nil {
				return err
			}
		}
	}
	return nil
}

func imports(pkg *build.Package, isRoot bool) map[string]bool {
	imps := make(map[string]bool)
	addPackages(imps, pkg.Imports)
	if isRoot && !*noTestDeps {
		addPackages(imps, pkg.TestImports)
		addPackages(imps, pkg.XTestImports)
	}
	return imps
}

func addPackages(m map[string]bool, ss []string) {
	for _, s := range ss {
		if *std || !isStdlib(s) {
			m[s] = true
		}
	}
}

// matchPattern(pattern)(name) reports whether
// name matches pattern.  Pattern is a limited glob
// pattern in which '...' means 'any string' and there
// is no other special syntax.
// Stolen from the go tool
func matchPattern(pattern string) func(name string) bool {
	re := regexp.QuoteMeta(pattern)
	re = strings.Replace(re, `\.\.\.`, `.*`, -1)
	// Special case: foo/... matches foo too.
	if strings.HasSuffix(re, `/.*`) {
		re = re[:len(re)-len(`/.*`)] + `(/.*)?`
	}
	reg := regexp.MustCompile(`^` + re + `$`)
	return func(name string) bool {
		return reg.MatchString(name)
	}
}
