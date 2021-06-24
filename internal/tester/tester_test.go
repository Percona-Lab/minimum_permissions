package tester

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"testing"

	tu "github.com/Percona-Lab/minimum_permissions/internal/testutils"
	_ "github.com/go-sql-driver/mysql"
	mysql "github.com/go-sql-driver/mysql"
)

var (
	dsn, templateDSN string
	db               *sql.DB
)

func TestMain(m *testing.M) {
	envDSN := os.Getenv("TEST_DSN")
	if envDSN == "" {
		log.Fatalf("TEST_DSN env var is empty")
	}

	cfg, err := mysql.ParseDSN(envDSN)
	if err != nil {
		log.Fatalf("Cannot parse TEST_DSN: %s", err)
	}
	cfg.AllowNativePasswords = true
	cfg.MultiStatements = true
	dsn = cfg.FormatDSN()

	templateDSN := fmt.Sprintf("%%s:%%s@%s(%s)/?autocommit=0", cfg.Net, cfg.Addr)
	log.Printf("Test DSN: %q", dsn)
	log.Printf("Template DSN: %q", templateDSN)

	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Cannot connect to the DB: %s", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatalf("Cannot ping the DB: %s", err)
	}

	os.Exit(m.Run())
}

func TestNewConnection(t *testing.T) {
	grants := []string{"SELECT", "UPDATE"}

	tc, err := NewTestConnection(db, templateDSN, grants)
	tu.IsNil(t, err)
	tu.Assert(t, tc != nil, "Test Connection is nil")

	var c int
	err = db.QueryRow("SELECT COUNT(*) FROM mysql.user WHERE host = '%' AND user = ?", tc.User()).Scan(&c)
	tu.IsNil(t, err)
	tu.Assert(t, c == 1, "User count should be 1")

	err = tc.Destroy()
	tu.IsNil(t, err)

	err = db.QueryRow("SELECT COUNT(*) FROM mysql.user WHERE host = '%' AND user = ?", tc.User()).Scan(&c)
	tu.IsNil(t, err)
	tu.Assert(t, c == 0, "User count should be 0")
}

func TestTestQueries(t *testing.T) {
	queries := []string{
		"SELECT `i`, COUNT(*) FROM `d1`.`t` WHERE 1=1 GROUP BY i ORDER BY i LOCK IN SHARE MODE",
		"insert into d1.t values (2)",
		"insert into d1.t values (2,3)",
	}

	expects := []struct {
		Queries []string
		Grants  []string
		OkCount int
	}{
		{Queries: queries, Grants: []string{"SELECT"}, OkCount: 0},           // #1
		{Queries: queries, Grants: []string{"SELECT", "UPDATE"}, OkCount: 1}, // #2
		{Queries: queries, Grants: []string{"INSERT"}, OkCount: 1},           // #4
		{Queries: queries, Grants: []string{"DELETE"}, OkCount: 0},           // #5
	}

	tu.LoadQueriesFromFile(t, "prep.sql")
	stopChan := make(chan bool)

	for i, test := range expects {
		testCases := []*TestingCase{}
		for _, query := range test.Queries {
			testCases = append(testCases, &TestingCase{Query: query})
		}

		tc, err := NewTestConnection(db, templateDSN, test.Grants)
		tu.IsNil(t, err)
		tu.Assert(t, tc != nil, "Test Connection is nil")

		okCount := tc.TestQueries(testCases, stopChan)

		tu.Assert(t, okCount == test.OkCount, fmt.Sprintf("#%d: OK count should be %d but is: %d",
			i+1, test.OkCount, okCount))

		tc.Destroy()
	}
}

func TestTestQuery(t *testing.T) {
	query := "SELECT `i`, COUNT(*) FROM `d1`.`t` WHERE 1=1 GROUP BY i ORDER BY i LOCK IN SHARE MODE"

	expects := []struct {
		Query        string
		Grants       []string
		OkCount      int
		InvalidQuery bool
	}{
		{Query: query, Grants: []string{"SELECT"}, OkCount: 0, InvalidQuery: false},                          // #1
		{Query: query, Grants: []string{"SELECT", "UPDATE"}, OkCount: 1, InvalidQuery: false},                // #2
		{Query: "insert into d1.t values (2)", Grants: []string{"SELECT"}, OkCount: 0, InvalidQuery: false},  // #3
		{Query: "insert into d1.t values (2)", Grants: []string{"INSERT"}, OkCount: 1, InvalidQuery: false},  // #4
		{Query: "insert into d1.t values (2,3)", Grants: []string{"INSERT"}, OkCount: 0, InvalidQuery: true}, // #5
	}

	tu.LoadQueriesFromFile(t, "prep.sql")

	for i, test := range expects {
		testCase := &TestingCase{Query: test.Query}

		tc, err := NewTestConnection(db, templateDSN, test.Grants)
		tu.IsNil(t, err)
		tu.Assert(t, tc != nil, "Test Connection is nil")

		wg := &sync.WaitGroup{}
		wg.Add(1)

		tc.testQuery(testCase, wg)

		wg.Wait()

		tu.Assert(t, test.InvalidQuery == testCase.InvalidQuery,
			fmt.Sprintf("#%d: InvalidQuery should be %v, but is: %v", i+1, test.InvalidQuery,
				testCase.InvalidQuery))

		tc.Destroy()
	}
}
