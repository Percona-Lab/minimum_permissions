package sandbox

// MySQL Minimum Permissions tool
// This package is based on dbdeployer by Giuseppe Maxia
// Visit https://github.com/datacharmer/mysql_minimum_permissions tool to check the original implementation

import (
	"database/sql"
	"os"
	"os/exec"
	"path"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestDeploy(t *testing.T) {

	sandboxDir := "/tmp/sandbox"

	baseDir := os.Getenv("MYSQL_BASEDIR")
	if baseDir == "" {
		msg := "Env var MYSQL_BASEDIR is not set. You need MySQL binaries and point to them " +
			" using the MYSQL_BASEDIR env var in order to run this test"
		t.Skip(msg)
	}

	sb := SandboxDef{
		DirName:           "dirname",
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
		Port:              5722,
		MysqlXPort:        0,
		UserPort:          0,
		BasePort:          0,
		MorePorts:         nil,
		Prompt:            "",
		DbUser:            "msandbox",
		RplUser:           "rsandbox",
		DbPassword:        "msandbox",
		RplPassword:       "msandbox",
		RemoteAccess:      "127.%",
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

	if fi, err := os.Stat(sandboxDir); err == nil && fi.IsDir() {
		os.RemoveAll(sandboxDir)
	}

	os.MkdirAll(sandboxDir, os.ModePerm)

	err := CreateSingleSandbox(sb)
	if err != nil {
		t.Errorf("Cannot start the sandbox: %s", err)
	}

	dsn := "root:msandbox@tcp(127.0.0.1:5722)/"
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("Cannot connect to the sandbox: %s", err)
	}
	time.Sleep(2 * time.Second) // Let the server start
	err = conn.Ping()
	if err != nil {
		t.Fatalf("Cannot ping the sandbox: %s", err)
	}

	query := "CREATE USER 'someuser'@'%' IDENTIFIED BY 'somepass'"
	_, err = conn.Exec(query)
	if err != nil {
		t.Errorf("Cannot create user: %s", err)
	}
	query = "GRANT SELECT, INSERT ON *.* to 'someuser'@'%';"
	_, err = conn.Exec(query)

	conn.Close()

	stopCmd := path.Join(os.TempDir(), "sandbox", "stop")
	cmd := exec.Command(stopCmd)
	err = cmd.Run()
	if err != nil {
		t.Fatalf("Cannot stop the sandbox: %s", err)
	}

	conn, err = sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("Cannot connect to the sandbox: %s", err)
	}
	time.Sleep(3 * time.Second)
	err = conn.Ping()
	if err == nil {
		t.Error("Database is still running")
	}
	conn.Close()

	if fi, err := os.Stat(sandboxDir); err == nil && fi.IsDir() {
		os.RemoveAll(sandboxDir)
	}
}

func TestGetMySQLVersion(t *testing.T) {

	binDir := "/home/karl/mysql/my-5.7/"
	v, err := getMySQLVersion(binDir)
	if err != nil {
		t.Errorf("Cannot get MySQL version: %s", err)
	}

	if v == nil {
		t.Errorf("MySQL version returned nil")
	}
}
