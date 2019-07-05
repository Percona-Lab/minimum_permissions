package testsandbox

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path"
	"reflect"
	"regexp"
	"runtime"
	"strings"

	"github.com/datacharmer/dbdeployer/sandbox"
	"github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type cleanupAction struct {
	Func func([]interface{}) error
	Args []interface{}
}

type TestSandbox struct {
	name           string
	workDir        string
	dbName         string
	db             *sql.DB
	host           string
	user           string
	password       string
	port           int
	templateDSN    string
	cleanupActions []*cleanupAction
	grants         []string
}

// New returns a new sandbox instance
func New(mysqlBaseDir string) (*TestSandbox, error) {
	var err error
	ts := &TestSandbox{
		cleanupActions: []*cleanupAction{},
		host:           "127.0.0.1",
		user:           "root",
		password:       "msandbox",
	}

	log.Debug().Msg("Trying go get a free open port")
	ts.port, err = getFreePort()
	if err != nil {
		return ts, errors.Wrapf(err, "cannot find a free open port")
	}
	log.Info().Msgf("Found free open port: %d", ts.port)

	// Create the sandbox directory
	ts.workDir, err = ioutil.TempDir("", "min_perms_")
	if err != nil {
		return ts, errors.Wrap(err, "cannot create a temporary directory for the sandbox")
	}
	ts.cleanupActions = append(ts.cleanupActions, &cleanupAction{Func: removeSandboxDir, Args: []interface{}{ts.workDir}})

	ts.name = fmt.Sprintf("sandbox_%d", ts.port)
	// Start the sandbox
	log.Info().Msgf("Sandbox dir : %s", ts.workDir)
	log.Info().Msgf("Sandbox name: %s", ts.name)
	log.Info().Msg("Starting the sandbox")

	if err := startSandbox(mysqlBaseDir, ts.workDir, ts.name, ts.port); err != nil {
		return ts, errors.Wrap(err, "cannot start the sandbox")
	}

	ts.cleanupActions = append(ts.cleanupActions, &cleanupAction{Func: stopSandbox, Args: []interface{}{ts.workDir, ts.name}})

	ts.db, err = getDBConnection(ts.host, ts.user, ts.password, ts.port)
	if err != nil {
		return ts, errors.Wrap(err, "cannot connect to the db")
	}
	ts.cleanupActions = append(ts.cleanupActions, &cleanupAction{Func: closeDB, Args: []interface{}{ts.db}})

	if v, e := validGrants(ts.db); !v || e != nil {
		if e != nil {
			return ts, errors.Wrap(err, "cannot check for valid grants")
		}
		return ts, errors.Wrap(err, "the user %q must have GRANT OPTION")
	}

	ts.dbName = fmt.Sprintf("min_perms_test_%04d", rand.Int63n(10000))
	log.Debug().Msgf("Testing database name: %s", ts.dbName)

	_, err = ts.db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", ts.dbName))
	createQuery := fmt.Sprintf("CREATE DATABASE `%s`", ts.dbName)
	log.Debug().Msgf("Exec: %q", createQuery)

	_, err = ts.db.Exec(createQuery)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot create the random database %q", ts.dbName)
	}

	ts.cleanupActions = append(ts.cleanupActions, &cleanupAction{Func: dropTempDB, Args: []interface{}{ts.db, ts.dbName}})

	if ts.grants, err = ts.getAllGrants(); err != nil {
		return ts, errors.Wrap(err, "cannot get all grants")
	}

	return ts, nil
}

func (ts *TestSandbox) DB() *sql.DB {
	return ts.db
}

// RunCleanupActions will execute all cleanup actions like drop temp dirs, close db connections, etc
func (ts *TestSandbox) RunCleanupActions() {
	log.Info().Msg("Cleaning up")
	log.Debug().Msgf("Cleanup actions list lenght: %d", len(ts.cleanupActions))
	for i := len(ts.cleanupActions) - 1; i >= 0; i-- {
		action := (ts.cleanupActions)[i]
		name := runtime.FuncForPC(reflect.ValueOf(action.Func).Pointer()).Name()
		log.Debug().Msgf("Running cleanup action #%d - %s", i, name)
		if err := action.Func(action.Args); err != nil {
			log.Error().Msgf("Cannot run cleanup action %s #%d: %s", name, i, err)
		}
	}
}

func (ts *TestSandbox) Grants() []string {
	return ts.grants
}

func (ts *TestSandbox) getAllGrants() ([]string, error) {
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
	err := ts.db.QueryRow("SELECT VERSION()").Scan(&vs)
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
	_, err = sandbox.CreateChildSandbox(sb)
	return err
}

func stopSandbox(args []interface{}) error {
	sandboxDir := args[0].(string)
	sandboxName := args[1].(string)
	_, err := sandbox.RemoveSandbox(sandboxDir, sandboxName, false)
	return err
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
