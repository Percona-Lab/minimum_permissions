package testsandbox

// func TestGetAllGrants57(t *testing.T) {
// 	tu.SkipIfGreatherThan(t, "5.7.99")
//
// 	want := []string{
// 		"SELECT", "INSERT", "DELETE", "UPDATE", "ALTER", "ALTER ROUTINE", "CREATE",
// 		"CREATE ROUTINE", "CREATE TABLESPACE", "CREATE TEMPORARY TABLES", "CREATE USER",
// 		"CREATE VIEW", "DROP", "EVENT", "EXECUTE", "FILE", "GRANT OPTION", "INDEX",
// 		"LOCK TABLES", "PROCESS", "REFERENCES", "RELOAD", "REPLICATION CLIENT",
// 		"REPLICATION SLAVE", "SHOW DATABASES", "SHOW VIEW", "SHUTDOWN ", "SUPER", "TRIGGER", "USAGE",
// 	}
//
// 	sandbox, err := testsandbox.New(opts.mysqlBaseDir)
// 	if err != nil {
// 		log.Fatal().Msgf("Cannot start the MySQL sandbox: %s", err)
// 	}
//
// 	userGrants, err := sandbox.Grants()
// 	tu.IsNil(t, err)
// 	tu.Equals(t, userGrants, want)
// }
