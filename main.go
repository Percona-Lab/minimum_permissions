package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/Percona-Lab/minimum_permissions/internal/qreader"
	"github.com/Percona-Lab/minimum_permissions/internal/report"
	"github.com/Percona-Lab/minimum_permissions/internal/tester"
	"github.com/Percona-Lab/minimum_permissions/internal/testsandbox"
	"github.com/Percona-Lab/minimum_permissions/internal/utils"
	"github.com/alecthomas/kingpin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
)

type cliOptions struct {
	mysqlBaseDir       string
	maxDepth           int
	noTrimLongQueries  bool
	trimQuerySize      int
	hideInvalidQueries bool
	keepSandbox        bool
	query              []string
	inputFile          string
	slowLog            string
	genLog             string
	showVersion        bool
	debug              bool
	quiet              bool
	host               string
	port               int
	user               string
	password           string
	sandboxDirname     string
}

var (
	Version   = "0.0.0."
	Commit    = "<sha1>"
	Branch    = "branch-name"
	Build     = "2017-01-01"
	GoVersion = "1.9.2"
)

type testResults struct {
	OkQueries      []*tester.TestingCase
	NotOkQueries   []*tester.TestingCase
	InvalidQueries []*tester.TestingCase
}

type resultGroups map[string][]string

func main() {
	opts, err := processCliArgs(os.Args[1:])

	if opts.showVersion {
		fmt.Printf("Version   : %s\n", Version)
		fmt.Printf("Commit    : %s\n", Commit)
		fmt.Printf("Branch    : %s\n", Branch)
		fmt.Printf("Build     : %s\n", Build)
		fmt.Printf("Go version: %s\n", GoVersion)
		return
	}

	if err != nil {
		log.Fatal().Msg(err.Error())
	}

	opts.mysqlBaseDir = utils.ExpandHomeDir(opts.mysqlBaseDir)
	if err := verifyBaseDir(opts.mysqlBaseDir); err != nil {
		log.Fatal().Msgf("MySQL binaries not found in %q", opts.mysqlBaseDir)
	}

	sandbox, err := testsandbox.New(opts.mysqlBaseDir)
	if !opts.keepSandbox {
		defer sandbox.RunCleanupActions()
	}
	if err != nil {
		log.Fatal().Msgf("Cannot start the MySQL sandbox: %s", err)
	}

	if terminal.IsTerminal(int(os.Stdout.Fd())) {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if opts.quiet {
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	}
	if opts.debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Info().Msg("Building the test cases list")
	testCases, err := buildTestCasesList(opts.query, opts.slowLog, opts.inputFile, opts.genLog)
	if err != nil {
		log.Fatal().Msgf("Cannot build the test cases list: %s", err)
	}
	if len(testCases) == 0 {
		log.Error().Msg("Test cases list is empty.")
		log.Fatal().Msg("Please use --slow-log and/or --input-file and/or --test-statement parameters")
	}
	log.Info().Msgf("Total number of queries to test: %d", len(testCases))

	log.Debug().Msg("Test cases:")
	for i, tc := range testCases {
		log.Debug().Msgf("%04d: %s", i, tc.Query)
	}

	grants := sandbox.Grants()
	// Start the spinner only if running in a terminal and if verbose has not been
	// specified, otherwise, the spinner will mess the output
	s := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	if terminal.IsTerminal(int(os.Stdout.Fd())) {
		if !opts.quiet && !opts.debug {
			s.Start()
		}
	}

	stopChan := make(chan bool)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	go func() {
		<-c
		close(stopChan)
		fmt.Println("CTRL+C detected. Finishing ...")
	}()

	results, invalidQueries := test(testCases, sandbox.DB(), templateDSN, grants, opts.maxDepth, stopChan, opts.quiet)

	if terminal.IsTerminal(int(os.Stdout.Fd())) && !opts.quiet && !opts.debug {
		s.Stop()
	}

	if !opts.hideInvalidQueries || opts.debug {
		report.PrintInvalidQueries(invalidQueries, os.Stdout)
		fmt.Println("\n")
	}

	if !opts.noTrimLongQueries {
		trimQueries(results, opts.trimQuerySize)
	}

	report.PrintReport(report.GroupResults(results), os.Stdout)
}

func buildTestCasesList(query []string, slowLog, plainFile, genLog string) ([]*tester.TestingCase, error) {
	testCases := []*tester.TestingCase{}

	if len(query) > 0 {
		log.Info().Msgf("Adding test statement to the queries list: %q", query)
		for _, query := range query {
			testCases = append(testCases, &tester.TestingCase{Query: query})
		}
	}

	if slowLog != "" {
		log.Info().Msgf("Adding queries from slow log file: %q", slowLog)
		tc, err := qreader.ReadSlowLog(slowLog)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot read slow log file %q", slowLog)
		}
		testCases = append(testCases, tc...)
	}

	if plainFile != "" {
		log.Info().Msgf("Adding queries from plain file: %q", plainFile)
		tc, err := qreader.ReadPlainFile(plainFile)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot read plain file %q", plainFile)
		}
		testCases = append(testCases, tc...)
	}

	if genLog != "" {
		log.Info().Msgf("Adding queries from genlog file: %q", genLog)
		tc, err := qreader.ReadGeneralLog(genLog)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot read genlog file %q", genLog)
		}
		testCases = append(testCases, tc...)
	}

	return testCases, nil
}

func test(testCases []*tester.TestingCase, db *sql.DB, templateDSN string, grants []string,
	maxDepth int, stopChan chan bool, quiet bool) ([]*tester.TestingCase, []*tester.TestingCase) {
	results := []*tester.TestingCase{}
	invalidQueries := []*tester.TestingCase{}
	stop := false

	totalQueries := len(testCases)
	progress := ""
	for n := 1; n < maxDepth && !stop; n++ {
		// grantsCombinations is a slice of slices having all combinations in groups of n
		// Example: n=2
		// [
		//   [SELECT, INSERT],
		//   [SELECT, UPDATE],
		//   ...
		//   [DELETE, UPDATE],
		//   ...
		// ]

		grantsCombinations := getGrantsCombinations(grants, n)

		for j := 0; j < len(grantsCombinations); j++ {
			grants := grantsCombinations[j]
			select {
			case <-stopChan:
				fmt.Println("")
				stop = true
				continue
			default:
			}
			testConn, e := tester.NewTestConnection(db, templateDSN, grants)
			if e != nil {
				log.Info().Msgf("Cannot grant this/these permissions to the test user: %v: %s", grants, e)
				log.Info().Msg("Skipping")
				if len(grants) == 1 {
					removeGrantFromList(grants, grants[0])
				}
				stop = true
				break
			}

			if len(progress) > 50 {
				progress = ""
			}
			progress = progress + "."
			cleanup := strings.Repeat(" ", 51-len(progress))
			found := len(results)
			invalid := len(invalidQueries)
			remaining := totalQueries - found - invalid
			if !quiet {
				fmt.Printf("Found GRANTS for %d queries. Invalid queries found: %d. Still testing %d queries. %s%s\r", found, invalid, remaining, progress, cleanup)
			}

			tr := testQueries(testConn, testCases, stopChan)

			testConn.Destroy()

			results = append(results, tr.OkQueries...)
			invalidQueries = append(invalidQueries, tr.InvalidQueries...)
			testCases = tr.NotOkQueries
			if len(testCases) == 0 {
				stop = true
				break
			}
		}
	}
	fmt.Println()

	return results, invalidQueries
}

// removeGrantFromList removes a specific grant from the list of grants.
// This is needed because not in all MySQL servers we can use all grants, for example
// SUPER is not enabled on Amazon RDS so, if we detect a specific grant cannot be used,
// we need to remove it from the list of grants to avoid including it in a combination
// with other grants to speed up the process.
func removeGrantFromList(grants []string, grant string) []string {
	for i := 0; i < len(grants); i++ {
		if grants[i] == grant {
			return append(grants[:i], grants[i+1:]...)
		}
	}
	return grants
}

func trimQueries(testCases []*tester.TestingCase, size int) {
	for _, tc := range testCases {
		if len(tc.Query) > size {
			tc.Query = tc.Query[:size] + " ... (truncated)"
		}
	}
}

func testQueries(testConn *tester.TestConnection, testCases []*tester.TestingCase, stopChan chan bool) testResults {
	tr := testResults{}

	testConn.TestQueries(testCases, stopChan)

	for _, tc := range testCases {
		if tc.MinimumGrants != nil {
			tr.OkQueries = append(tr.OkQueries, tc)
			continue
		}
		if tc.InvalidQuery {
			tr.InvalidQueries = append(tr.InvalidQueries, tc)
			continue
		}
		tr.NotOkQueries = append(tr.NotOkQueries, tc)
	}

	return tr
}

func getGrantsCombinations(grants []string, length int) [][]string {
	grantsArray := [][]string{}

	combinations := comb(len(grants), length)

	for _, combRow := range combinations {
		grantsList := []string{}
		for _, grant := range combRow {
			grantsList = append(grantsList, grants[grant])
		}
		grantsArray = append(grantsArray, grantsList)
	}
	return grantsArray
}

func comb(n, m int) [][]int {
	s := make([]int, m)
	combinations := [][]int{}

	last := m - 1
	var rc func(int, int)
	rc = func(i, next int) {
		for j := next; j < n; j++ {
			s[i] = j
			if i == last {
				ss := make([]int, len(s))
				copy(ss, s)
				combinations = append(combinations, ss)
			} else {
				rc(i+1, j+1)
			}
		}
		// return
	}
	rc(0, 0)

	return combinations
}

func prepare(db *sql.DB, prepareFile string) error {
	if _, err := os.Stat(prepareFile); err != nil {
		return errors.Wrapf(err, "Cannot read input file %q", prepareFile)
	}

	cmds, err := ioutil.ReadFile(prepareFile)
	if err != nil {
		return errors.Wrap(err, "Cannot prepare environment")
	}

	_, err = db.Exec(string(cmds))
	if err != nil {
		return errors.Wrap(err, "Cannot prepare environment")
	}

	return nil
}

func verifyBaseDir(dir string) error {
	mysqlBin := filepath.Join(dir, "bin", "mysqld")
	fi, err := os.Stat(mysqlBin)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("Invalid mysql binaries path")
	}
	return nil
}

func processCliArgs(args []string) (cliOptions, error) {
	opts := cliOptions{
		query: make([]string, 0),
	}
	app := kingpin.New("mysql_random_data_loader", "MySQL Random Data Loader")
	app.HelpFlag.Short('h')

	app.Flag("mysql-base-dir", "Path to the MySQL base directory").Required().StringVar(&opts.mysqlBaseDir)
	app.Flag("max-depth", "Maximum number of permissions to try").Default("10").IntVar(&opts.maxDepth)
	app.Flag("no-trim-long-queries", "Do not trim long queries").BoolVar(&opts.noTrimLongQueries)
	app.Flag("trim-query-size", "Trim queries longer than trim-query-size").Default("100").IntVar(&opts.trimQuerySize)
	app.Flag("hide-invalid-queries", "Don't show invalid queries in the final report").BoolVar(&opts.hideInvalidQueries)
	app.Flag("keep-sandbox", "Do not stop/remove the sandbox after finishing").BoolVar(&opts.keepSandbox)

	app.Flag("query", "Query to test. Can be specified multiple times").Short('q').StringsVar(&opts.query)
	app.Flag("input-file",
		"Load queries from plain text file. Queries in this file must end with a ; and can have multiple lines").
		Short('i').StringVar(&opts.inputFile)
	app.Flag("slow-log", "Load queries from slow log file").Short('s').StringVar(&opts.slowLog)
	app.Flag("gen-log", "Load queries from genlog file").Short('g').StringVar(&opts.genLog)

	app.Flag("version", "Show version and exit").BoolVar(&opts.showVersion)
	app.Flag("debug", "Debug mode").BoolVar(&opts.debug)
	app.Flag("quiet", "Don't show info level notificacions and progress").BoolVar(&opts.quiet)

	app.Flag("sandbox-dirname", "Directory name for the sandbox").Default("sandbox").StringVar(&opts.sandboxDirname)

	_, err := app.Parse(args)
	return opts, err
}
