package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/datacharmer/dbdeployer/sandbox"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/Percona-Lab/minimum_permissions/internal/qreader"
	"github.com/Percona-Lab/minimum_permissions/internal/report"
	"github.com/Percona-Lab/minimum_permissions/internal/tester"
	"github.com/Percona-Lab/minimum_permissions/internal/utils"
	"github.com/alecthomas/kingpin"
	_ "github.com/go-sql-driver/mysql"
	version "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
)

type cleanupAction struct {
	Func func([]interface{}) error
	Args []interface{}
}

var (
	app = kingpin.New("mysql_random_data_loader", "MySQL Random Data Loader")

	mysqlBaseDir       = app.Flag("mysql-base-dir", "Path to the MySQL base directory (parent of bin/)").Required().String()
	maxDepth           = app.Flag("max-depth", "Maximum number of permissions to try").Default("10").Int()
	noTrimLongQueries  = app.Flag("no-trim-long-queries", "Do not trim long queries").Bool()
	trimQuerySize      = app.Flag("trim-query-size", "Trim queries longer than trim-query-size").Default("100").Int()
	hideInvalidQueries = app.Flag("hide-invalid-queries", "Don't show invalid queries in the final report").Bool()
	keepSandbox        = app.Flag("keep-sandbox", "Do not stop/remove the sandbox after finishing").Bool()

	query     = app.Flag("query", "Query to test. Can be specified multiple times").Short('q').Strings()
	inputFile = app.Flag("input-file", "Load queries from plain text file. Queries in this file must end with a ; and can have multiple lines").Short('i').String()
	slowLog   = app.Flag("slow-log", "Load queries from slow log file").Short('s').String()
	genLog    = app.Flag("gen-log", "Load queries from genlog file").Short('g').String()

	showVersion = app.Flag("version", "Show version and exit").Bool()
	debug       = app.Flag("debug", "Debug mode").Bool()
	quiet       = app.Flag("quiet", "Don't show info level notificacions and progress").Bool()

	host           = "127.0.0.1"
	port           = 0
	user           = "root"
	password       = "msandbox"
	sandboxDirName = "sandbox"

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
	// Enable -h to show help
	app.HelpFlag.Short('h')

	_, err := app.Parse(os.Args[1:])

	if *showVersion {
		fmt.Printf("Version   : %s\n", Version)
		fmt.Printf("Commit    : %s\n", Commit)
		fmt.Printf("Branch    : %s\n", Branch)
		fmt.Printf("Build     : %s\n", Build)
		fmt.Printf("Go version: %s\n", GoVersion)
		return
	}

	if err != nil {
		log.Fatal().Msg(err.Error())
		app.Usage(os.Args[1:])
	}

	// This will store a list of functions to execute before existing the program
	// They must be executed in reverse order
	cleanupActions := []*cleanupAction{}
	defer runCleanupActions(&cleanupActions)

	*mysqlBaseDir = utils.ExpandHomeDir(*mysqlBaseDir)
	if err := verifyBaseDir(*mysqlBaseDir); err != nil {
		log.Fatal().Msgf("MySQL binaries not found in %q", *mysqlBaseDir)
	}

	if terminal.IsTerminal(int(os.Stdout.Fd())) {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if *quiet {
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	}
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Info().Msg("Building the test cases list")
	testCases, err := buildTestCasesList(*query, *slowLog, *inputFile, *genLog)
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

	// Find free open port for the sandbox
	log.Debug().Msg("Trying go get a free open port")
	port, err = getFreePort()
	if err != nil {
		log.Fatal().Msgf("Cannot find a free open port %s", err)
	}
	log.Info().Msgf("Found free open port: %d", port)

	// Create the sandbox directory
	sandboxDir, err := ioutil.TempDir("", "min_perms_")
	if err != nil {
		log.Fatal().Msgf("Cannot create a temporary directory for the sandbox: %s", err)
	}
	if !*keepSandbox {
		cleanupActions = append(cleanupActions, &cleanupAction{Func: removeSandboxDir, Args: []interface{}{sandboxDir}})
	}

	sandboxName := fmt.Sprintf("sandbox_%d", port)
	// Start the sandbox
	log.Info().Msgf("Sandbox dir : %s", sandboxDir)
	log.Info().Msgf("Sandbox name: %s", sandboxName)
	log.Info().Msg("Starting the sandbox")

	if err := startSandbox(*mysqlBaseDir, sandboxDir, sandboxName, port); err != nil {
		log.Fatal().Msgf("Cannot start the sandbox: %s", err)
	}

	if !*keepSandbox {
		cleanupActions = append(cleanupActions, &cleanupAction{Func: stopSandbox, Args: []interface{}{sandboxDir, sandboxName}})
	}

	db, err := getDBConnection(host, user, password, port)
	if err != nil {
		log.Fatal().Msgf("Cannot connect to the db: %s", err)
	}
	cleanupActions = append(cleanupActions, &cleanupAction{Func: closeDB, Args: []interface{}{db}})

	if v, e := validGrants(db); !v || e != nil {
		if e != nil {
			log.Fatal().Msg(e.Error())
		}
		log.Fatal().Msgf("The user %q must have GRANT OPTION", user)
	}

	randomDB := fmt.Sprintf("min_perms_test_%04d", rand.Int63n(10000))
	log.Debug().Msgf("Testing database name: %s", randomDB)

	_, err = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", randomDB))
	createQuery := fmt.Sprintf("CREATE DATABASE `%s`", randomDB)
	log.Debug().Msgf("Exec: %q", createQuery)
	_, err = db.Exec(createQuery)
	if err != nil {
		log.Fatal().Msgf("Cannot create the random database %q: %s", randomDB, err)
	}
	cleanupActions = append(cleanupActions, &cleanupAction{Func: dropTempDB, Args: []interface{}{db, randomDB}})

	templateDSN := getTemplateDSN(host, port, randomDB)
	log.Debug().Msgf("Template DSN used for client connections: %q", templateDSN)

	grants, err := getAllGrants(db)
	if err != nil {
		log.Fatal().Msgf("Cannot get grants list: %s", err)
	}
	log.Debug().Msgf("Grants to test:\n%+v", grants)

	// Start the spinner only if running in a terminal and if verbose has not been
	// specified, otherwise, the spinner will mess the output
	s := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	if terminal.IsTerminal(int(os.Stdout.Fd())) {
		if !*quiet && !*debug {
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

	results, invalidQueries := test(testCases, db, templateDSN, grants, *maxDepth, stopChan, *quiet)

	if terminal.IsTerminal(int(os.Stdout.Fd())) && !*quiet && !*debug {
		s.Stop()
	}
	if !*hideInvalidQueries || *debug {
		report.PrintInvalidQueries(invalidQueries, os.Stdout)
		fmt.Println("\n")
	}

	if !*noTrimLongQueries {
		trimQueries(results, *trimQuerySize)
	}

	report.PrintReport(report.GroupResults(results), os.Stdout)
}

func closeDB(args []interface{}) error {
	db := args[0].(*sql.DB)
	return db.Close()
}

func dropTempDB(args []interface{}) error {
	conn := args[0].(*sql.DB)
	dbName := args[1].(string)
	log.Debug().Msgf("Dropping db %q", dbName)
	_, err := conn.Exec(fmt.Sprintf("DROP DATABASE `%s`", dbName))
	return err
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

func getFreePort() (int, error) {
	loopback, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	listener, err := net.ListenTCP("tcp", loopback)
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func runCleanupActions(actions *[]*cleanupAction) {
	log.Info().Msg("Cleaning up")
	log.Debug().Msgf("Cleanup actions list lenght: %d", len(*actions))
	for i := len(*actions) - 1; i >= 0; i-- {
		action := (*actions)[i]
		name := runtime.FuncForPC(reflect.ValueOf(action.Func).Pointer()).Name()
		log.Debug().Msgf("Running cleanup action #%d - %s", i, name)
		if err := action.Func(action.Args); err != nil {
			log.Error().Msgf("Cannot run cleanup action %s #%d: %s", name, i, err)
		}
	}
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
		select {
		case <-stopChan:
			stop = true
		default:
		}

		grantsCombinations := getGrantsCombinations(grants, n)

		for j := 0; j < len(grantsCombinations) && !stop; j++ {
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

func getAllGrants(db *sql.DB) ([]string, error) {
	grants := []string{"SELECT", "INSERT", "DELETE", "UPDATE", "CREATE", "ALTER", "DROP",
		"CREATE TEMPORARY TABLES", "ALTER ROUTINE", "CREATE ROUTINE", "CREATE TABLESPACE",
		"CREATE USER", "CREATE VIEW", "EVENT", "EXECUTE", "FILE",
		"GRANT OPTION", "INDEX", "LOCK TABLES", "PROCESS",
		"REFERENCES", "RELOAD", "REPLICATION CLIENT", "REPLICATION SLAVE",
		"SHOW DATABASES", "SHOW VIEW", "SHUTDOWN ", "SUPER",
		"TRIGGER", "USAGE",
	}

	// Permissible Dynamic Privileges for GRANT and REVOKE (MySQL 8.0+)
	// https://dev.mysql.com/doc/refman/8.0/en/grant.html#grant-privileges
	mysql8Grants := []string{"BINLOG_ADMIN", "CONNECTION_ADMIN",
		"ENCRYPTION_KEY_ADMIN",
		"GROUP_REPLICATION_ADMIN", "REPLICATION_SLAVE_ADMIN", "ROLE_ADMIN",
		"SET_USER_ID", "SYSTEM_VARIABLES_ADMIN",
	}

	var vs string
	err := db.QueryRow("SELECT VERSION()").Scan(&vs)
	if err != nil {
		return nil, err
	}

	v, err := version.NewVersion(vs)
	if err != nil {
		return nil, err
	}

	v80, _ := version.NewVersion("8.0.0")
	if !v.LessThan(v80) { // there is no >= in version pkg
		grants = append(grants, mysql8Grants...)
	}

	return grants, nil
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

func getDBConnection(host, user, password string, port int) (*sql.DB, error) {
	protocol, hostPort := getProtocolAndHost(host, port)
	dsn := fmt.Sprintf("%s:%s@%s(%s)/", user, password, protocol, hostPort)
	log.Debug().Msgf("Connecting to the database using DSN: %s", dsn)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, errors.Wrapf(err, "Cannot connect to the db using %q", dsn)
	}

	if err = db.Ping(); err != nil {
		return nil, errors.Wrapf(err, "Cannot connect to the db using %q", dsn)
	}
	db.SetMaxOpenConns(1)

	return db, nil
}

func getTemplateDSN(host string, port int, database string) string {
	protocol, hostPort := getProtocolAndHost(host, port)
	//return fmt.Sprintf("%%s:%%s@%s(%s)/%s", protocol, hostPort, database)
	return fmt.Sprintf("%%s:%%s@%s(%s)/", protocol, hostPort)
}

func getProtocolAndHost(host string, port int) (string, string) {
	protocol := "tcp"
	hostPort := host

	if host == "localhost" {
		protocol = "unix"
	} else {
		hostPort = fmt.Sprintf("%s:%d", host, port)
	}
	return protocol, hostPort
}

func validGrants(db *sql.DB) (bool, error) {
	var grants string
	err := db.QueryRow("SHOW GRANTS").Scan(&grants)
	if err != nil {
		return false, errors.Wrap(err, "Cannot get grants")
	}
	if strings.Contains(grants, "WITH GRANT OPTION") {
		return true, nil
	}
	return false, nil
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

func removeSandboxDir(args []interface{}) error {
	log.Info().Msgf("Removing sandbox dir %s", args[0].(string))
	return os.RemoveAll(args[0].(string))
}

func startSandbox(baseDir, sandboxDir, sandboxName string, port int) error {
	ver, err := getMySQLVersion(baseDir)
	if err != nil {
		return errors.Wrapf(err, "cannot get MySQL version from base dir: %s", baseDir)
	}
	sb := sandbox.SandboxDef{
		SandboxDir:       sandboxDir, // this should be /tmp on Linux
		DirName:          sandboxName,
		SBType:           "single",
		LoadGrants:       true,
		Version:          ver.String(),
		Basedir:          baseDir,
		Port:             port,
		DbUser:           "msandbox",
		DbPassword:       "msandbox",
		RplUser:          "rsandbox",
		RplPassword:      "rsandbox",
		RemoteAccess:     "127.0.0.1",
		BindAddress:      "127.0.0.1",
		NativeAuthPlugin: true,
		DisableMysqlX:    true,
		KeepUuid:         true,
		SinglePrimary:    true,
		Force:            true,
	}

	log.Debug().Msgf("Creating the base directory for the sandbox %q", sandboxDir)
	if err := os.MkdirAll(sandboxDir, os.ModePerm); err != nil {
		return errors.Wrapf(err, "cannot create temporary directory for the sandbox: %s", sandboxDir)
	}
	sandbox.CreateChildSandbox(sb)
	return err
}

func stopSandbox(args []interface{}) error {
	sandboxDir := args[0].(string)
	sandboxName := args[1].(string)
	sandbox.RemoveSandbox(sandboxDir, sandboxName, false)
	return nil
}

func getMySQLVersion(baseDir string) (*version.Version, error) {
	mysqld := path.Join(baseDir, "bin", "mysqld")
	cmd := exec.Command(mysqld, "--version")
	log.Debug().Msgf("Trying to get MySQL from mysqld: %s --version", mysqld)

	buf, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`(?i)Ver (\d+\.\d+\.\d+)\s*`)
	m := re.FindStringSubmatch(string(buf))
	if len(m) < 2 {
		return nil, fmt.Errorf("Cannot parse MySQL server version")
	}

	log.Debug().Msgf("Version found: %s", m[1])
	v, err := version.NewVersion(m[1])
	return v, err
}
