package main

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/rs/zerolog/log"

	tu "github.com/Percona-Lab/minimum_permissions/internal/testutils"
)

var dsn, templateDSN string

func TestMain(m *testing.M) {
	envDSN := os.Getenv("TEST_DSN")
	if envDSN == "" {
		log.Fatal().Msg("TEST_DSN env var is empty")
	}

	cfg, err := mysql.ParseDSN(envDSN)
	if err != nil {
		log.Fatal().Msgf("Cannot parse TEST_DSN: %s", err)
	}
	cfg.AllowNativePasswords = true
	cfg.MultiStatements = true
	dsn = cfg.FormatDSN()

	templateDSN := fmt.Sprintf("%%s:%%s@%s(%s)/?autocommit=0", cfg.Net, cfg.Addr)
	log.Printf("Test DSN: %q", dsn)
	log.Printf("Template DSN: %q", templateDSN)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal().Msgf("Cannot connect to the DB: %s", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatal().Msgf("Cannot ping the DB: %s", err)
	}

	os.Exit(m.Run())
}

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
	want := [][]int{{0, 1}, {0, 2}, {1, 2}}
	cmb := comb(3, 2)
	tu.Equals(t, cmb, want)
}

// func TestGetAllGrants57(t *testing.T) {
// 	tu.SkipIfGreatherThan(t, "5.7.99")
// 	db := tu.GetMySQLConnection(t)
//
// 	want := []string{
// 		"SELECT", "INSERT", "DELETE", "UPDATE", "ALTER", "ALTER ROUTINE", "CREATE",
// 		"CREATE ROUTINE", "CREATE TABLESPACE", "CREATE TEMPORARY TABLES", "CREATE USER",
// 		"CREATE VIEW", "DROP", "EVENT", "EXECUTE", "FILE", "GRANT OPTION", "INDEX",
// 		"LOCK TABLES", "PROCESS", "REFERENCES", "RELOAD", "REPLICATION CLIENT",
// 		"REPLICATION SLAVE", "SHOW DATABASES", "SHOW VIEW", "SHUTDOWN ", "SUPER", "TRIGGER", "USAGE",
// 	}
//
// 	userGrants, err := sandbox.Grants()
// 	tu.IsNil(t, err)
// 	tu.Equals(t, userGrants, want)
// }
//
// func TestGetAllGrants80(t *testing.T) {
// 	tu.SkipIfLessThan(t, "8.0")
// 	db := tu.GetMySQLConnection(t)
//
// 	want := []string{
// 		"SELECT", "INSERT", "DELETE", "UPDATE", "ALTER", "ALTER ROUTINE", "CREATE",
// 		"CREATE ROUTINE", "CREATE TABLESPACE", "CREATE TEMPORARY TABLES", "CREATE USER",
// 		"CREATE VIEW", "DROP", "EVENT", "EXECUTE", "FILE", "GRANT OPTION", "INDEX",
// 		"LOCK TABLES", "PROCESS", "REFERENCES", "RELOAD", "REPLICATION CLIENT",
// 		"REPLICATION SLAVE", "SHOW DATABASES", "SHOW VIEW", "SHUTDOWN ", "SUPER",
// 		"TRIGGER", "USAGE",
// 		// MySQL 8 Permissible Dynamic Privileges for GRANT and REVOKE
// 		"BINLOG_ADMIN", "CONNECTION_ADMIN", "ENCRYPTION_KEY_ADMIN", "GROUP_REPLICATION_ADMIN",
// 		"REPLICATION_SLAVE_ADMIN", "ROLE_ADMIN", "SET_USER_ID", "SYSTEM_VARIABLES_ADMIN",
// 	}
//
// 	userGrants, err := getAllGrants(db)
// 	tu.IsNil(t, err)
// 	tu.Equals(t, userGrants, want)
// }
//
// func TestAllGrants(t *testing.T) {
// 	tc := []*tester.TestingCase{
// 		{Query: "DROP DATABASE testdb"},
// 	}
// 	stopChan := make(chan bool)
// 	cfg := tu.GetDSN(t)
//
// 	dsn := fmt.Sprintf("%s:%s@%s(%s)/?multiStatements=true", "root", "", "tcp", cfg.Addr)
// 	templateDSN := fmt.Sprintf("%%s:%%s@%s(%s)/%s?autocommit=0", "tcp", cfg.Addr, "test")
//
// 	db, err := sql.Open("mysql", dsn)
// 	db.Exec("CREATE DATABASE IF NOT EXISTS test")
// 	tu.IsNil(t, err, fmt.Sprintf("Cannot connect to the db using %q", dsn))
//
// 	grants := []string{
// 		"SELECT", "INSERT", "UPDATE", "DELETE", "ALTER", "ALTER ROUTINE", "CREATE", "CREATE ROUTINE",
// 		"CREATE TABLESPACE", "CREATE TEMPORARY TABLES", "CREATE USER", "CREATE VIEW", "DROP", "EVENT",
// 		"EXECUTE", "FILE", "GRANT OPTION", "INDEX", "LOCK TABLES", "PROCESS", "REFERENCES", "RELOAD",
// 		"REPLICATION CLIENT", "REPLICATION SLAVE", "SHOW DATABASES", "SHOW VIEW", "SHUTDOWN ",
// 		"SUPER", "TRIGGER", "USAGE",
// 	}
// 	r, i := test(tc, db, templateDSN, grants, 5, stopChan)
//
// 	want := []*tester.TestingCase{
// 		{
// 			Database:         "",
// 			Query:            "DROP DATABASE testdb",
// 			Fingerprint:      "",
// 			MinimumGrants:    []string{"DROP"},
// 			LastTestedGrants: []string{"DROP"},
// 			NotAllowed:       false,
// 			Error:            &mysql.MySQLError{Number: 0x3f0, Message: "Can't drop database 'testdb'; database doesn't exist"},
// 			InvalidQuery:     false,
// 		},
// 	}
//
// 	tu.Equals(t, r, want)
// 	tu.Equals(t, len(i), 0)
// }
//
// func TestReadSlowLog(t *testing.T) {
// 	tc, err := readSlowLog("testdata/slow_80_small.log")
//
// 	tu.IsNil(t, err, "Cannot read slow log")
//
// 	tu.Equals(t, len(tc), 39)
// }
//
// func TestTestFunc(t *testing.T) {
// 	testCases := []*tester.TestingCase{
// 		{
// 			Database:         "",
// 			Query:            "DROP DATABASE testdb",
// 			Fingerprint:      "",
// 			MinimumGrants:    []string{"DROP"},
// 			LastTestedGrants: []string{"DROP"},
// 			NotAllowed:       false,
// 			Error:            &mysql.MySQLError{Number: 0x3f0, Message: "Can't drop database 'testdb'; database doesn't exist"},
// 			InvalidQuery:     false,
// 		},
// 		{
// 			Query: "INSERT INTO pt_osc.t (id, c) VALUES ('502', 'new row 1530033539.17197');",
// 		},
// 		{
// 			Query: "DELETE FROM pt_osc.t WHERE id='226';",
// 		},
// 		{
// 			Query: "SELECT /*!40001 SQL_NO_CACHE */ `id` FROM `pt_osc`.`t` FORCE INDEX (`PRIMARY`) WHERE `id` IS NOT NULL ORDER BY `id` LIMIT 1 /*key_len*/;",
// 		},
// 	}
// 	grants := []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
// 	stopChan := make(chan bool)
// 	maxDepth := 2
//
// 	results, invalidQueries := test(testCases, db, templateDSN, grants, maxDepth, stopChan)
// 	pretty.Println(results)
// 	pretty.Println(invalidQueries)
// }
//
// func TestFuncWithSandbox(t *testing.T) {
// 	zerolog.SetGlobalLevel(zerolog.DebugLevel)
// 	sandboxDir, err := ioutil.TempDir("", "example")
// 	if err != nil {
// 		t.FailNow()
// 	}
// 	log.Printf("Using temp dir: %s", sandboxDir)
// 	port := 8112
// 	baseDir := "/home/karl/mysql/my-8.0"
//
// 	if envPort := os.Getenv("EXTERNAL_SANDBOX_PORT"); envPort != "" {
// 		port, err = strconv.Atoi(envPort)
// 		log.Printf("Using external sandbox instance at port %d", port)
// 	} else {
// 		if envBaseDir := os.Getenv("MYSQL_BASE_DIR"); envBaseDir != "" {
// 			baseDir = envBaseDir
// 		}
// 		log.Printf("Using internal sandbox instance at port %d, using binaries at %q", port, baseDir)
// 		startSandbox(baseDir, sandboxDir, port)
// 	}
//
// 	protocol := "tcp"
// 	dsn := fmt.Sprintf("root:msandbox@tcp(127.0.0.1:%d)/", port)
// 	templateDSN := fmt.Sprintf("%%s:%%s@%s(127.0.0.1:%d)/", protocol, port)
//
// 	db, err := sql.Open("mysql", dsn)
// 	if err != nil {
// 		log.Printf("Cannot connect to %q: %s", dsn, err)
// 		t.FailNow()
// 	}
//
// 	testCases := []*tester.TestingCase{
// 		{
// 			Database:         "",
// 			Query:            "DROP DATABASE testdb",
// 			Fingerprint:      "",
// 			MinimumGrants:    []string{"DROP"},
// 			LastTestedGrants: []string{"DROP"},
// 			NotAllowed:       false,
// 			Error:            &mysql.MySQLError{Number: 0x3f0, Message: "Can't drop database 'testdb'; database doesn't exist"},
// 			InvalidQuery:     false,
// 		},
// 		{
// 			Query: "INSERT INTO pt_osc.t (id, c) VALUES ('502', 'new row 1530033539.17197');",
// 		},
// 		{
// 			Query: "DELETE FROM pt_osc.t WHERE id='226';",
// 		},
// 		{
// 			Query: "SELECT /*!40001 SQL_NO_CACHE */ `id` FROM `pt_osc`.`t` FORCE INDEX (`PRIMARY`) WHERE `id` IS NOT NULL ORDER BY `id` LIMIT 1 /*key_len*/;",
// 		},
// 	}
// 	grants := []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
// 	stopChan := make(chan bool)
// 	maxDepth := len(grants)
//
// 	log.Info().Msg("----------------------------------------------------------------------------------------------------")
// 	results, invalidQueries := test(testCases, db, templateDSN, grants, maxDepth, stopChan)
// 	pretty.Println(results)
// 	pretty.Println(invalidQueries)
// }
