package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/signal"
	"time"

	"github.com/briandowns/spinner"
	"github.com/kr/pretty"
	slo "github.com/percona/go-mysql/log"
	"github.com/percona/go-mysql/query"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/Percona-Lab/minimum_permissions/internal/report"
	"github.com/Percona-Lab/minimum_permissions/internal/tester"
	"github.com/alecthomas/kingpin"
	_ "github.com/go-sql-driver/mysql"
	version "github.com/hashicorp/go-version"
	"github.com/percona/go-mysql/log/slow"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var (
	app = kingpin.New("mysql_random_data_loader", "MySQL Random Data Loader")

	debug             = app.Flag("debug", "Debug mode").Bool()
	database          = app.Flag("database", "Default database name").String()
	host              = app.Flag("host", "Host name/IP").Short('h').Default("127.0.0.1").String()
	maxDepth          = app.Flag("max-depth", "Maximum number of permissions to try").Default("10").Int()
	pass              = app.Flag("password", "Password").Short('p').String()
	port              = app.Flag("port", "Port").Short('P').Default("3306").Int()
	prepareFile       = app.Flag("prepare-file", "File having queries to run before start").String()
	testStatement     = app.Flag("test-statement", "Query to test").Strings()
	noTrimLongQueries = app.Flag("no-trim-long-queris", "Do not trim long queries").Bool()
	showVersion       = app.Flag("version", "Show version and exit").Bool()
	slowLog           = app.Flag("slow-log", "Slow log file").ExistingFile()
	trimQuerySize     = app.Flag("trim-query-size", "Trim queries longer than trim-query-size").Default("100").Int()
	user              = app.Flag("user", "User").Short('u').String()
	verbose           = app.Flag("verbose", "Show all permissions being tested").Bool()

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
		log.Errorln(err)
		os.Exit(1)
	}

	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.SetLevel(log.ErrorLevel)
	if *verbose {
		log.SetLevel(log.DebugLevel)
	}

	protocol := "tcp"
	hostPort := *host

	if *host == "localhost" {
		protocol = "unix"
	} else {
		hostPort = fmt.Sprintf("%s:%d", *host, *port)
	}

	dsn := fmt.Sprintf("%s:%s@%s(%s)/%s?multiStatements=true", *user, *pass, protocol, hostPort, *database)
	log.Debugf("Connecting to the database using DSN: %s", dsn)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Cannot connect to the db using %q: %s", dsn, err)
	}
	if err = db.Ping(); err != nil {
		log.Fatalf("Cannot connect to the db using %q: %s", dsn, err)
	}
	defer db.Close()
	kingpin.Usage()

	randomDB := fmt.Sprintf("min_perms_test_%04d", rand.Int63n(10000))
	createQuery := fmt.Sprintf("CREATE DATABASE `%s`", randomDB)

	log.Debugf("Exec: %q", createQuery)
	_, err = db.Exec(createQuery)
	if err != nil {
		log.Fatalf("Cannot create the random database %q: %s", randomDB, err)
	}

	templateDSN := fmt.Sprintf("%%s:%%s@%s(%s)/%s?autocommit=0", protocol, hostPort, randomDB)

	if *prepareFile != "" {
		if err = prepare(db, *prepareFile); err != nil {
			log.Fatalf("Cannot prepare the environment: %s", err.Error())
		}
	}

	testCases := []*tester.TestingCase{}

	if *slowLog == "" && len(*testStatement) > 0 {
		for _, query := range *testStatement {
			testCases = append(testCases, &tester.TestingCase{Query: query})
		}
	}

	if *slowLog != "" {
		testCases, err = readSlowLog(*slowLog)
		if err != nil {
			log.Fatalf("Cannot read slow log from %q: %s", *slowLog, err)
		}
	}

	grants, err := getAllGrants(db)
	if err != nil {
		log.Fatalf("Cannot get grants list: %s", err)
	}

	s := spinner.New(spinner.CharSets[0], 100*time.Millisecond) // Build our new spinner
	if terminal.IsTerminal(int(os.Stdout.Fd())) && !*verbose {
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

	log.Debugf("Dropping db %q", randomDB)
	db.Exec(fmt.Sprintf("DROP DATABASE `%s`", randomDB))

	if terminal.IsTerminal(int(os.Stdout.Fd())) && !*verbose {
		s.Stop()
	}

	if *debug {
		fmt.Println("Invalid Queries ====================================================================================================")
		pretty.Print(invalidQueries)
		fmt.Println("Remaining Test Cases ===============================================================================================")
		pretty.Print(testCases)
		fmt.Println("====================================================================================================================")
	}

	if !*noTrimLongQueries {
		trimQueries(results, *trimQuerySize)
	}

	report.PrintReport(report.GroupResults(results), os.Stdout)
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
				log.Infof("Cannot grant this/these permissions to the test user: %v: %s", grants, e)
				log.Info("Skipping")
				if len(grants) == 1 {
					removeGrantFromList(grants, grants[0])
				}
				continue
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
