Showdeps - an opinionated tool for inspecting Go package dependencies
----------------

Showdeps starts where `go list -f` leaves off. It's useful for exploring
dependency graphs of Go programs.

By default, showdeps just shows the packages imported by the packages
named on the command line. It doesn't show those packages themselves,
it doesn't recursively visit all dependencies, and it doesn't show
dependencies in the standard library.

You can specify additional flags to show all of those things:

The `-a` flag will show all dependencies recursively.  By default
this will include testing dependencies but only those of the packages
specifically mentioned.  This keeps the dependency graph from becoming
too unwieldy due the testing dependencies in external repositories that
you really don't care about.

The `-stdlib` flag will include dependencies from the standard
library. These are excluded by default because dependencies on the
standard library are rarely a problem.

The `-T` flag causes test dependencies to be omitted.  With `-T`
specified, you'll see the exact dependencies of the package without
pollution from test-related code.

There are other flags that provide more insight into the details of
the dependencies.

The `-from` flag shows *why* each dependency is included by printing,
along with each package, the list of packages that depend on it.

The `-why` flag can be used to explore just why a particular package has
been included in the result. By default, it shows a single dependency chain
(a sequence of package names that import each other) from
the listed packages to any package matched by the `=why` argument.
If the -a flag is provided, it instead prints all packages
that are part of any of those chains in `-from` style.

Finally, the `-f` flag causes all the Go source file names to be printed.
Since this is usually for whole-program greps or analysis, this also
includes the source files in the packages specified on the command line.


Examples:
--------

Print the immediate dependencies of the package
in the current directory:

	$ showdeps


Print the import pages of all the packages used directly and indirectly by net/http.

	$ showdeps -a -stdlib -T net/http

Show a line count of all the packages underneath
the current directory and their dependencies.

	$ showdeps -a -f ./... | xargs cat | wc -l

Find out one reason why net/http indirectly imports crypto/x509/pkix:

	$ showdeps -T  -why crypto/x509/pkix net/http
	net/http crypto/tls crypto/x509 crypto/x509/pkix
