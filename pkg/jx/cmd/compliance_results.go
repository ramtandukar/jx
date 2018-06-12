package cmd

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/heptio/sonobuoy/pkg/client"
	"github.com/heptio/sonobuoy/pkg/client/results"
	"github.com/jenkins-x/jx/pkg/jx/cmd/templates"
	cmdutil "github.com/jenkins-x/jx/pkg/jx/cmd/util"
	"github.com/onsi/ginkgo/reporters"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	complianceResultsLong = templates.LongDesc(`
		Shows the results of the compliance tests
	`)

	complianceResultsExample = templates.Examples(`
		# Show the compliance results
		jx compliance results
	`)
)

// ComplianceStatusOptions options for "compliance results" command
type ComplianceResultsOptions struct {
	CommonOptions
}

// NewCmdComplianceResults creates a command object for the "compliance results" action, which
// shows the results of E2E compliance tests
func NewCmdComplianceResults(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := &ComplianceResultsOptions{
		CommonOptions: CommonOptions{
			Factory: f,
			Out:     out,
			Err:     errOut,
		},
	}

	cmd := &cobra.Command{
		Use:     "results",
		Short:   "Shows the results of compliance tests",
		Long:    complianceResultsLong,
		Example: complianceResultsExample,
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			cmdutil.CheckErr(err)
		},
	}

	return cmd
}

// Run implements the "compliance results" command
func (o *ComplianceResultsOptions) Run() error {
	cc, err := o.Factory.CreateComplianceClient()
	if err != nil {
		return errors.Wrap(err, "could not create the compliance client")
	}

	cfg := &client.RetrieveConfig{
		Namespace: complianceNamespace,
	}

	reader, err := cc.RetrieveResults(cfg)
	if err != nil {
		return errors.Wrap(err, "could not retrieve the compliance results")
	}

	resultsReader, err := untarResults(reader)
	if err != nil {
		return errors.Wrap(err, "could not extract the compliance results from archive")
	}

	gzr, err := gzip.NewReader(resultsReader)
	if err != nil {
		return errors.Wrap(err, "could not create a gzip reader for compliance results ")
	}

	results, err := cc.GetTests(gzr, "all")
	if err != nil {
		return errors.Wrap(err, "could not get the results of the compliance tests from the archive")
	}
	sort.Sort(StatusSortedTestCases(results))
	return o.printResults(results)
}

// StatusSotedTestCase implements Sort by status of a list of test case
type StatusSortedTestCases []reporters.JUnitTestCase

var statuses = map[string]int{
	"FAILED":  0,
	"PASSED":  1,
	"SKIPPED": 2,
	"UNKNOWN": 3,
}

func (s StatusSortedTestCases) Len() int { return len(s) }
func (s StatusSortedTestCases) Less(i, j int) bool {
	si := statuses[status(s[i])]
	sj := statuses[status(s[j])]
	return si < sj
}
func (s StatusSortedTestCases) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (o *ComplianceResultsOptions) printResults(junitResults []reporters.JUnitTestCase) error {
	writer := &tabwriter.Writer{}
	writer.Init(os.Stdout, 0, 0, 0, ' ', 0)
	fmt.Fprintln(writer, "STATUS \tTEST\t")
	for _, t := range junitResults {
		fmt.Fprintf(writer, "%s \t%s\t\n", status(t), t.Name)
	}
	return writer.Flush()
}

func status(junitResult reporters.JUnitTestCase) string {
	if results.Skipped(junitResult) {
		return "SKIPPED"
	} else if results.Failed(junitResult) {
		return "FAILED"
	} else if results.Passed(junitResult) {
		return "PASSED"
	} else {
		return "UNKNOWN"
	}
}

func untarResults(src io.Reader) (io.Reader, error) {
	tarReader := tar.NewReader(src)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
			break
		}
		if strings.HasSuffix(header.Name, ".tar.gz") {
			reader, writer := io.Pipe()
			//TODO propagate the error out of the goroutine
			go func(writer *io.PipeWriter) {
				defer writer.Close()
				io.Copy(writer, tarReader)
			}(writer)
			return reader, nil
		}
	}
	return nil, errors.New("no compliance results archive found")
}
