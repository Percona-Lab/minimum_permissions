package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	slo "github.com/percona/go-mysql/log"
	"github.com/percona/go-mysql/query"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/Percona-Lab/minimum_permissions/internal/report"
	"github.com/Percona-Lab/minimum_permissions/internal/tester"
	"github.com/Percona-Lab/minimum_permissions/internal/utils"
	"github.com/Percona-Lab/minimum_permissions/sandbox"
	"github.com/alecthomas/kingpin"
	_ "github.com/go-sql-driver/mysql"
	version "github.com/hashicorp/go-version"
	"github.com/percona/go-mysql/log/slow"
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
	prepareFile        = app.Flag("prepare-file", "File with queries to run before starting").String()
	testStatement      = app.Flag("test-statement", "Query to test").Strings()
	noTrimLongQueries  = app.Flag("no-trim-long-queries", "Do not trim long queries").Bool()
	keepSandbox        = app.Flag("keep-sandbox", "Do not stop/remove the sandbox after finishing").Bool()
	slowLog            = app.Flag("slow-log", "Test queries from this slow log file").ExistingFile()
	inputFile          = app.Flag("input-file", "Plain text file with input queries. Queries in this file must end with a ;").String()
	trimQuerySize      = app.Flag("trim-query-size", "Trim queries longer than trim-query-size").Default("100").Int()
	showInvalidQueries = app.Flag("show-invalid-queries", "Show invalid queries").Bool()

	showVersion = app.Flag("version", "Show version and exit").Bool()
	debug       = app.Flag("debug", "Debug mode").Bool()
	verbose     = app.Flag("verbose", "Show all permissions being tested").Bool()

	host           = "127.0.0.1"
	port           = 0
	user           = "msandbox"
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
	// This will store a list of functions to execute before existing the program
	// They must be executed in reverse order
	cleanupActions := []*cleanupAction{}
	defer runCleanupActions(&cleanupActions)

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
		os.Exit(1)
	}

	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	if *verbose {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Info().Msg("Building the test cases list")
	testCases, err := buildTestCasesList(*testStatement, *slowLog, *inputFile)
	if err != nil {
		log.Fatal().Msgf("Cannot build the test cases list: %s", err)
	}
	if len(testCases) == 0 {
		log.Error().Msg("Test cases list is empty.")
		log.Fatal().Msg("Please use --slow-log and/or --input-file and/or --test-statement parameters")
	}

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
	sandboxDir, err := ioutil.TempDir("", "sandbox")
	if err != nil {
		log.Fatal().Msgf("Cannot create a temporary directory for the sandbox: %s", err)
	}
	if !*keepSandbox {
		cleanupActions = append(cleanupActions, &cleanupAction{Func: removeSandboxDir, Args: []interface{}{sandboxDir}})
	}

	log.Info().Msgf("Sandbox dir: %s", sandboxDir)
	log.Info().Msg("Starting the sandbox")
	startSandbox(*mysqlBaseDir, sandboxDir, port)
	if !*keepSandbox {
		cleanupActions = append(cleanupActions, &cleanupAction{Func: stopSandbox, Args: []interface{}{sandboxDir}})
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

	createQuery := fmt.Sprintf("CREATE DATABASE `%s`", randomDB)
	log.Debug().Msgf("Exec: %q", createQuery)
	_, err = db.Exec(createQuery)
	if err != nil {
		log.Fatal().Msgf("Cannot create the random database %q: %s", randomDB, err)
	}
	cleanupActions = append(cleanupActions, &cleanupAction{Func: dropTempDB, Args: []interface{}{db, randomDB}})

	templateDSN := getTemplateDSN(host, port, randomDB)
	log.Debug().Msgf("Template DSN used for client connections: %q", templateDSN)

	if *prepareFile != "" {
		log.Info().Msgf("Running prepare file %q", *prepareFile)
		if err = prepare(db, *prepareFile); err != nil {
			log.Fatal().Msgf("Cannot prepare the environment: %s", err.Error())
		}
	}

	grants, err := getAllGrants(db)
	if err != nil {
		log.Fatal().Msgf("Cannot get grants list: %s", err)
	}
	log.Debug().Msgf("Grants to test:\n%+v", grants)

	s := spinner.New(spinner.CharSets[0], 100*time.Millisecond) // Build our new spinner
	if terminal.IsTerminal(int(os.Stdout.Fd())) && !*verbose {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
		s.Start()
	}

	stopChan := make(chan bool)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	go func() {
		<-c
		close(stopChan)
		fmt.Println("CTRL+C detected. Finishing ...")
	}()

	results, invalidQueries := test(testCases, db, templateDSN, grants, *maxDepth, stopChan)

	if terminal.IsTerminal(int(os.Stdout.Fd())) && !*verbose {
		s.Stop()
	}

	if *showInvalidQueries || *debug {
		report.PrintInvalidQueries(invalidQueries, os.Stdout)
		fmt.Println("")
		fmt.Println("")
	}

	if !*noTrimLongQueries {
		trimQueries(results, *trimQuerySize)
	}

	report.PrintReport(report.GroupResults(results), os.Stdout)
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

func closeDB(args []interface{}) error {
	db := args[0].(*sql.DB)
	return db.Close()
}

func removeSandboxDir(args []interface{}) error {
	log.Info().Msgf("Removing sandbox dir %s", args[0].(string))
	return os.RemoveAll(args[0].(string))
}

func dropTempDB(args []interface{}) error {
	conn := args[0].(*sql.DB)
	dbName := args[1].(string)
	log.Debug().Msgf("Dropping db %q", dbName)
	_, err := conn.Exec(fmt.Sprintf("DROP DATABASE `%s`", dbName))
	return err
}

func stopSandbox(args []interface{}) error {
	sandboxDir := args[0].(string)
	stopCmd := path.Join(sandboxDir, "stop")
	cmd := exec.Command(stopCmd)
	log.Info().Msg("Stopping the sandbox")
	log.Debug().Msgf("Sandbox stop command: %q", stopCmd)
	return cmd.Run()
}

func buildTestCasesList(testStatement []string, slowLog, plainFile string) ([]*tester.TestingCase, error) {
	testCases := []*tester.TestingCase{}

	if len(testStatement) > 0 {
		log.Info().Msgf("Adding test statement to the queries list: %q", testStatement)
		for _, query := range testStatement {
			testCases = append(testCases, &tester.TestingCase{Query: query})
		}
	}

	if slowLog != "" {
		log.Info().Msgf("Adding queries from slow log file: %q", slowLog)
		tc, err := readSlowLog(slowLog)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot read slow log from %q", slowLog)
		}
		testCases = append(testCases, tc...)
	}

	if plainFile != "" {
		log.Info().Msgf("Adding queries from plain file: %q", plainFile)
		tc, err := readSlowLog(plainFile)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot read slow log from %q", inputFile)
		}
		testCases = append(testCases, tc...)
	}

	return testCases, nil
}

func test(testCases []*tester.TestingCase, db *sql.DB, templateDSN string, grants []string,
	maxDepth int, stopChan chan bool) ([]*tester.TestingCase, []*tester.TestingCase) {
	results := []*tester.TestingCase{}
	invalidQueries := []*tester.TestingCase{}
	stop := false

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
	grants := []string{"SELECT", "INSERT", "DELETE", "UPDATE", "ALTER",
		"ALTER ROUTINE", "CREATE", "CREATE ROUTINE", "CREATE TABLESPACE",
		"CREATE TEMPORARY TABLES", "CREATE USER",
		"CREATE VIEW", "DROP", "EVENT", "EXECUTE", "FILE",
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

func readSlowLog(filename string) ([]*tester.TestingCase, error) {
	filename = utils.ExpandHomeDir(filename)
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	slp := slow.NewSlowLogParser(file, slo.Options{Debug: false})

	go slp.Start()

	queryGroups := make(map[string]*slo.Event)

	for e := range slp.EventChan() {
		fp := query.Fingerprint(e.Query)
		queryGroups[fp] = e
	}

	testCases := []*tester.TestingCase{}
	for fingerprint, event := range queryGroups {
		testCases = append(testCases, &tester.TestingCase{Database: event.Db, Query: event.Query, Fingerprint: fingerprint})
	}
	slp.Stop()

	return testCases, nil
}

func readFlatFile(filename string) ([]*tester.TestingCase, error) {
	filename = utils.ExpandHomeDir(filename)
	file, err := os.Open(filename)
	if err != nil {
		return nil, errors.Wrapf(err, "Cannot open %s", filename)
	}
	defer file.Close()

	tc := []*tester.TestingCase{}
	lines := []string{}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fmt.Println(scanner.Text()) // token in unicode-char
		lines = append(lines, scanner.Text())
	}

	queries := joinQueryLines(lines)

	for _, query := range queries {
		tc = append(tc, &tester.TestingCase{Query: query})
	}

	return tc, errors.New("Not implemented yet")
}

func joinQueryLines(lines []string) []string {
	inQuery := false
	joined := []string{}
	queryString := ""
	separator := ""

	for _, line := range lines {
		if !inQuery {
			inQuery = true
		}
		if inQuery {
			queryString += separator + line
			separator = "\n"
			if !strings.HasSuffix(strings.TrimSpace(line), ";") {
				continue
			}
			inQuery = false
			separator = ""
			joined = append(joined, queryString)
			queryString = ""
			continue
		}
		joined = append(joined, line)
	}
	return joined
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
	db.SetMaxOpenConns(10)

	return db, nil
}

func getTemplateDSN(host string, port int, database string) string {
	protocol, hostPort := getProtocolAndHost(host, port)
	return fmt.Sprintf("%%s:%%s@%s(%s)/%s", protocol, hostPort, database)
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

func startSandbox(baseDir, sandboxDir string, port int) {

	sb := sandbox.SandboxDef{
		DirName:           sandboxDirName,
		SBType:            "single",
		Multi:             false,
		NodeNum:           1,
		Version:           "5.7.21",
		Basedir:           baseDir,
		SandboxDir:        sandboxDir,
		LoadGrants:        true,
		SkipReportHost:    false,
		SkipReportPort:    false,
		SkipStart:         false,
		InstalledPorts:    []int{},
		Port:              port,
		MysqlXPort:        0,
		UserPort:          0,
		BasePort:          0,
		MorePorts:         nil,
		Prompt:            "",
		DbUser:            "msandbox",
		RplUser:           "",
		DbPassword:        "msandbox",
		RplPassword:       "",
		RemoteAccess:      "",
		BindAddress:       "127.0.0.1",
		CustomMysqld:      "",
		ServerId:          1,
		ReplOptions:       "",
		GtidOptions:       "",
		SemiSyncOptions:   "",
		InitOptions:       []string{},
		MyCnfOptions:      []string{},
		PreGrantsSql:      []string{},
		PreGrantsSqlFile:  "",
		PostGrantsSql:     []string{},
		PostGrantsSqlFile: "",
		MyCnfFile:         "",
		NativeAuthPlugin:  true,
		DisableMysqlX:     true,
		KeepUuid:          true,
		SinglePrimary:     true,
		Force:             true,
		ExposeDdTables:    false,
		RunConcurrently:   false,
	}

	log.Debug().Msgf("Creating the base directory for the sandbox %q", sandboxDir)
	os.MkdirAll(sandboxDir, os.ModePerm)
	sandbox.CreateSingleSandbox(sb)
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
