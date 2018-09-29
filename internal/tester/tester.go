package tester

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type TestConnection struct {
	dsnTemplate string
	mainConn    *sql.DB
	grants      []string
	testConn    *sql.DB
	testDSN     string
	testUser    string
	testPass    string
}

type TestingCase struct {
	Database         string
	Query            string
	Fingerprint      string
	MinimumGrants    []string
	LastTestedGrants []string
	NotAllowed       bool
	Error            error
	InvalidQuery     bool
}

func NewTestConnection(conn *sql.DB, dsnTemplate string, grants []string) (*TestConnection, error) {
	tc := &TestConnection{
		testUser:    "someuser", //utils.RandomString(12),
		testPass:    "somepass", //utils.RandomString(12),
		mainConn:    conn,
		dsnTemplate: dsnTemplate,
		grants:      grants,
	}
	if conn == nil {
		return nil, fmt.Errorf("Main MySQL connection is nil")
	}
	// Drop the user just in case it exists. Don't check for errors because it might not exit.
	_, err := tc.mainConn.Exec(fmt.Sprintf("DROP USER '%s'@'%%'", tc.testUser))

	log.Debug().Msg(strings.Repeat("-", 100))
	query := fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", tc.testUser, tc.testPass)
	log.Debug().Msg(query)
	_, err = tc.mainConn.Exec(query)
	if err != nil {
		return nil, errors.Wrapf(err, "Cannot create a new testing user: %q", query)
	}
	query = fmt.Sprintf("GRANT %s ON *.* TO '%s'@'%%'", strings.Join(grants, ", "), tc.testUser)
	log.Debug().Msg(query)
	log.Debug().Msg(strings.Repeat("-", 100))

	_, err = tc.mainConn.Exec(query)
	if err != nil {
		return nil, errors.Wrapf(err, "Cannot GRANT privileges: %q", query)
	}

	tc.testDSN = fmt.Sprintf(dsnTemplate, tc.testUser, tc.testPass)
	tc.testConn, err = sql.Open("mysql", tc.testDSN)
	if err != nil {
		return nil, errors.Wrapf(err, "Cannot connect to the db using the test connection %q", tc.testDSN)
	}

	tc.testConn.SetMaxOpenConns(1)
	tc.testConn.SetMaxIdleConns(1)
	return tc, nil
}

func (tc *TestConnection) TestQueries(testCases []*TestingCase, stopChan chan bool) int {
	wg := sync.WaitGroup{}

	stop := false
	for i := 0; i < len(testCases) && !stop; i++ {
		testCase := testCases[i]
		select {
		case <-stopChan:
			stop = true
			continue
		default:
		}
		// If we know from a previous run that this is an invalid query, don't test it again
		if testCase.InvalidQuery {
			continue
		}
		wg.Add(1)
		//go tc.testQuery(testCase, &wg)
		tc.testQuery(testCase, &wg)
	}
	wg.Wait()

	// okCount holds the number of queries in the list that can be executed
	// with the current granted permissions
	okCount := 0
	for _, testCase := range testCases {
		// If there was an error, reset it so this query will be re-tested
		// on the next iteration
		if testCase.NotAllowed || testCase.Error != nil {
			continue
		}
		testCase.MinimumGrants = tc.grants
		okCount++
	}
	return okCount
}

func (tc *TestConnection) testQuery(testCase *TestingCase, wg *sync.WaitGroup) {
	defer wg.Done()
	tx, err := tc.testConn.Begin()
	if err != nil {
		testCase.Error = err
		return
	}
	defer tx.Rollback()

	testCase.Error = nil
	testCase.NotAllowed = false
	_, err = tx.Exec(testCase.Query)

	if err == nil {
		testCase.MinimumGrants = tc.grants
		//tx.Rollback()
	} else {
		testCase.Error = err
		testCase.LastTestedGrants = tc.grants

		me, _ := err.(*mysql.MySQLError)
		switch me.Number {
		case 1064: // Syntax error
			testCase.InvalidQuery = true
			break
		// 1044: Access denied for user '%s'@'%s' to database '%s'
		// 1045: Access denied for user '%s'@'%s' to database '%s' (Example: LOAD DATA INFILE)
		// 1095: Kill denied (You are not owner of thread %lu)
		// 1142: Table access denied (%s command denied to user '%s'@'%s' for table '%s')
		// 1143: Access denied to column (%s command denied to user '%s'@'%s' for column '%s' in table '%s')
		// 1227 (0x4cb): Specific access denied (you need (at least one of) the %s privilege(s) for this operation)
		// 1419 (0x58b): You do not have the SUPER privilege and binary logging is enabled
		// 1370: Process access denied (%s command denied to user '%s'@'%s' for routine '%s')
		// 1873: Access denied: change user (Access denied trying to change to user '%s'@'%s' (using password: %s). Disconnecting.)
		// 3202: Keyring access denied. (Access denied; you need %s privileges for this operation)
		case 1044, 1045, 1095, 1142, 1143, 1227, 1419: //, 1370, 1873, 3202:
			testCase.NotAllowed = true
			break
		// For these, we know for sure the query can be executed
		// 1049: Database doesn't exists
		// 1067 (0x42B): Invalid default value for
		// 1146: Table doesn't exists
		// 1213 (0x4bd): Deadlock
		// 1215 (0x4bf): Cannot add FK constraint
		// 1231 (0x4cf): Invalid value for variable
		case 1049, 1146, 1067, 1213, 1215, 1231: // Database doesn't exist but it was able to run the query
			testCase.MinimumGrants = tc.grants
			break
		default:
			testCase.MinimumGrants = tc.grants
		}
	}
}

func (tc *TestConnection) User() string {
	return tc.testUser
}

func (tc *TestConnection) Destroy() error {
	if tc.mainConn == nil {
		return nil
	}
	tc.testConn.Close()

	query := fmt.Sprintf("DROP USER `%s`@`%%`", tc.testUser)
	_, err := tc.mainConn.Exec(query)
	if err != nil {
		return errors.Wrap(err, "Cannot destroy test connection")
	}
	return nil
}
