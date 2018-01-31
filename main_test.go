package main

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/Percona-Lab/pt-mysql-config-diff/testutils"
)

func TestGetGrantsCombinations(t *testing.T) {
	grants := []string{"SUPER", "INSERT", "UPDATE"}
	cmb := getGrantsCombinations(grants, 2)
	want := [][]string{
		{"SUPER", "INSERT"},
		{"SUPER", "UPDATE"},
		{"INSERT", "UPDATE"},
	}
	testutils.Equals(t, cmb, want)
}

func TestCombinationsIndex(t *testing.T) {
	want := [][]int{[]int{0, 1}, []int{0, 2}, []int{1, 2}}
	cmb := comb(3, 2)
	testutils.Equals(t, cmb, want)
}

func TestGetAllGrants(t *testing.T) {
	dsn := fmt.Sprintf("%s:%s@%s(%s)/?multiStatements=true", "root", "", "tcp", "127.0.0.1:3308")
	db, err := sql.Open("mysql", dsn)
	testutils.IsNil(t, err, fmt.Sprintf("Cannot connect to the db using %q", dsn))

	want := []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP",
		"RELOAD", "SHUTDOWN", "PROCESS", "FILE", "REFERENCES",
		"INDEX", "ALTER", "SHOW DATABASES", "SUPER", "CREATE TEMPORARY TABLES",
		"LOCK TABLES", "EXECUTE", "REPLICATION SLAVE",
		"REPLICATION CLIENT", "CREATE VIEW", "SHOW VIEW", "CREATE ROUTINE",
		"ALTER ROUTINE", "CREATE USER", "EVENT", "TRIGGER", "CREATE TABLESPACE",
		"CREATE ROLE", "DROP ROLE", "BACKUP_ADMIN", "BINLOG_ADMIN",
		"CONNECTION_ADMIN", "ENCRYPTION_KEY_ADMIN",
		"GROUP_REPLICATION_ADMIN", "PERSIST_RO_VARIABLES_ADMIN", "REPLICATION_SLAVE_ADMIN",
		"RESOURCE_GROUP_ADMIN", "RESOURCE_GROUP_USER", "ROLE_ADMIN",
		"SET_USER_ID", "SYSTEM_VARIABLES_ADMIN", "XA_RECOVER_ADMIN",
	}

	userGrants, err := getAllGrants(db)
	testutils.IsNil(t, err)
	testutils.Equals(t, userGrants, want)
}
