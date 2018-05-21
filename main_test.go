package main

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/Percona-Lab/minimum_permissions/internal/tester"
	tu "github.com/Percona-Lab/minimum_permissions/internal/testutils"
	"github.com/go-sql-driver/mysql"
)

func TestGetGrantsCombinations(t *testing.T) {
	grants := []string{"SUPER", "INSERT", "UPDATE"}
	cmb := getGrantsCombinations(grants, 2)
	want := [][]string{
		{"SUPER", "INSERT"},
		{"SUPER", "UPDATE"},
		{"INSERT", "UPDATE"},
	}
	tu.Equals(t, cmb, want)
}

func TestCombinationsIndex(t *testing.T) {
	want := [][]int{[]int{0, 1}, []int{0, 2}, []int{1, 2}}
	cmb := comb(3, 2)
	tu.Equals(t, cmb, want)
}

func TestGetAllGrants57(t *testing.T) {
	tu.SkipIfGreatherThan(t, "5.7.99")
	db := tu.GetMySQLConnection(t)

	want := []string{"SELECT", "INSERT", "DELETE", "UPDATE", "ALTER", "ALTER ROUTINE", "CREATE",
		"CREATE ROUTINE", "CREATE TABLESPACE", "CREATE TEMPORARY TABLES", "CREATE USER",
		"CREATE VIEW", "DROP", "EVENT", "EXECUTE", "FILE", "GRANT OPTION", "INDEX",
		"LOCK TABLES", "PROCESS", "REFERENCES", "RELOAD", "REPLICATION CLIENT",
		"REPLICATION SLAVE", "SHOW DATABASES", "SHOW VIEW", "SHUTDOWN ", "SUPER", "TRIGGER", "USAGE",
	}

	userGrants, err := getAllGrants(db)
	tu.IsNil(t, err)
	tu.Equals(t, userGrants, want)
}

func TestGetAllGrants80(t *testing.T) {
	tu.SkipIfLessThan(t, "8.0")
	db := tu.GetMySQLConnection(t)

	want := []string{"SELECT", "INSERT", "DELETE", "UPDATE", "ALTER", "ALTER ROUTINE", "CREATE",
		"CREATE ROUTINE", "CREATE TABLESPACE", "CREATE TEMPORARY TABLES", "CREATE USER",
		"CREATE VIEW", "DROP", "EVENT", "EXECUTE", "FILE", "GRANT OPTION", "INDEX",
		"LOCK TABLES", "PROCESS", "REFERENCES", "RELOAD", "REPLICATION CLIENT",
		"REPLICATION SLAVE", "SHOW DATABASES", "SHOW VIEW", "SHUTDOWN ", "SUPER",
		"TRIGGER", "USAGE",
		// MySQL 8 Permissible Dynamic Privileges for GRANT and REVOKE
		"BINLOG_ADMIN", "CONNECTION_ADMIN", "ENCRYPTION_KEY_ADMIN", "GROUP_REPLICATION_ADMIN",
		"REPLICATION_SLAVE_ADMIN", "ROLE_ADMIN", "SET_USER_ID", "SYSTEM_VARIABLES_ADMIN",
	}

	userGrants, err := getAllGrants(db)
	tu.IsNil(t, err)
	tu.Equals(t, userGrants, want)
}

func TestAllGrants(t *testing.T) {
	tc := []*tester.TestingCase{
		{Query: "DROP DATABASE testdb"},
	}
	stopChan := make(chan bool)
	cfg := tu.GetDSN(t)

	dsn := fmt.Sprintf("%s:%s@%s(%s)/?multiStatements=true", "root", "", "tcp", cfg.Addr)
	templateDSN := fmt.Sprintf("%%s:%%s@%s(%s)/%s?autocommit=0", "tcp", cfg.Addr, "test")

	db, err := sql.Open("mysql", dsn)
	db.Exec("CREATE DATABASE IF NOT EXISTS test")
	tu.IsNil(t, err, fmt.Sprintf("Cannot connect to the db using %q", dsn))

	grants := []string{
		"SELECT", "INSERT", "UPDATE", "DELETE", "ALTER", "ALTER ROUTINE", "CREATE", "CREATE ROUTINE",
		"CREATE TABLESPACE", "CREATE TEMPORARY TABLES", "CREATE USER", "CREATE VIEW", "DROP", "EVENT",
		"EXECUTE", "FILE", "GRANT OPTION", "INDEX", "LOCK TABLES", "PROCESS", "REFERENCES", "RELOAD",
		"REPLICATION CLIENT", "REPLICATION SLAVE", "SHOW DATABASES", "SHOW VIEW", "SHUTDOWN ",
		"SUPER", "TRIGGER", "USAGE",
	}
	r, i := test(tc, db, templateDSN, grants, 5, stopChan)

	want := []*tester.TestingCase{
		&tester.TestingCase{
			Database:         "",
			Query:            "DROP DATABASE testdb",
			Fingerprint:      "",
			MinimumGrants:    []string{"DROP"},
			LastTestedGrants: []string{"DROP"},
			NotAllowed:       false,
			Error:            &mysql.MySQLError{Number: 0x3f0, Message: "Can't drop database 'testdb'; database doesn't exist"},
			InvalidQuery:     false,
		},
	}

	tu.Equals(t, r, want)
	tu.Equals(t, len(i), 0)
}

func TestReadSlowLog(t *testing.T) {
	tc, err := readSlowLog("testdata/slow_80_small.log")

	tu.IsNil(t, err, "Cannot read slow log")

	tu.Equals(t, len(tc), 39)
}

//func TestSandbox(t *testing.T) {
//	startSandbox()
//}
