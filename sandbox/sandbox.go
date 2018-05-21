package sandbox

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"text/template"

	"github.com/Percona-Lab/minimum_permissions/util"
	version "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	uuid "github.com/satori/go.uuid"
)

type SandboxDef struct {
	DirName           string
	SBType            string
	Multi             bool
	NodeNum           int
	Version           string
	Basedir           string
	SandboxDir        string
	LoadGrants        bool
	SkipReportHost    bool
	SkipReportPort    bool
	SkipStart         bool
	InstalledPorts    []int
	Port              int
	MysqlXPort        int
	UserPort          int
	BasePort          int
	MorePorts         []int
	Prompt            string
	DbUser            string
	RplUser           string
	DbPassword        string
	RplPassword       string
	RemoteAccess      string
	BindAddress       string
	CustomMysqld      string
	ServerId          int
	ReplOptions       string
	GtidOptions       string
	SemiSyncOptions   string
	InitOptions       []string
	MyCnfOptions      []string
	PreGrantsSql      []string
	PreGrantsSqlFile  string
	PostGrantsSql     []string
	PostGrantsSqlFile string
	MyCnfFile         string
	NativeAuthPlugin  bool
	DisableMysqlX     bool
	KeepUuid          bool
	SinglePrimary     bool
	Force             bool
	ExposeDdTables    bool
	RunConcurrently   bool
}

var (
	v569, _  = version.NewVersion("5.6.9")
	v570, _  = version.NewVersion("5.7.0")
	v576, _  = version.NewVersion("5.7.6")
	v800, _  = version.NewVersion("8.0.0")
	v804, _  = version.NewVersion("8.0.4")
	v8011, _ = version.NewVersion("8.0.11")
)

func FixServerUuid(sdef SandboxDef) (uuid_file, new_uuid string) {
	sbVersion, _ := version.NewVersion(sdef.Version)

	if sbVersion.LessThan(v569) {
		return
	}
	new_uuid = fmt.Sprintf("server-uuid=%s", uuid.NewV4().String())
	operation_dir := path.Join(sdef.SandboxDir, "data")
	uuid_file = path.Join(operation_dir, "auto.cnf")
	return
}

func CreateSingleSandbox(sdef SandboxDef) (err error) {
	if info, err := os.Stat(sdef.Basedir); err != nil || !info.IsDir() {
		return fmt.Errorf("Base directory %s does not exist", sdef.Basedir)
	}

	if sdef.Prompt == "" {
		sdef.Prompt = "mysql"
	}

	datadir := path.Join(sdef.SandboxDir, "data")
	tmpdir := path.Join(sdef.SandboxDir, "/tmp")
	log.Debug().Msgf("Sandbox dir: %s", datadir)
	log.Debug().Msgf("Temp dir: %s", tmpdir)

	mysqldVersion, err := getMySQLVersion(sdef.Basedir)
	if err != nil {
		return err
	}

	if sdef.ExposeDdTables {
		log.Debug().Msg("ExposedDdTables was specified")
		if mysqldVersion.LessThan(v800) {
			return fmt.Errorf("ExposeDdTables requires MySQL 8.0.0+")
		}
		sdef.PostGrantsSql = append(sdef.PostGrantsSql, SingleTemplates["expose_dd_tables"].Contents)
		if sdef.CustomMysqld != "" && sdef.CustomMysqld != "mysqld-debug" {
			msg := "ExposedDBTables requires mysqld-debug. A different file was indicated (custom-mysqld=%s)\n" +
				"Either use mysqld-debug or remove CustomMysqld"
			return fmt.Errorf(msg, sdef.CustomMysqld)
		}
		sdef.CustomMysqld = "mysqld-debug"
	}
	if sdef.CustomMysqld != "" {
		custom_mysqld := path.Join(sdef.Basedir, "bin", sdef.CustomMysqld)
		if _, err = exec.LookPath(custom_mysqld); err != nil {
			return fmt.Errorf("Cannot find executable for %s: %s", custom_mysqld, err)
		}
	}
	//if !mysqldVersion.LessThan(v804) {
	//	log.Debug().Msg("Using MySQL 8.0.4. Setting default_authentication_plugin=mysql_native_password")
	//	if sdef.NativeAuthPlugin == true {
	//		sdef.InitOptions = append(sdef.InitOptions, "--default_authentication_plugin=mysql_native_password")
	//		sdef.MyCnfOptions = append(sdef.MyCnfOptions, "default_authentication_plugin=mysql_native_password")
	//	}
	//}
	if !mysqldVersion.LessThan(v8011) {
		log.Debug().Msg("MySQL version is 8.0.11+")
		if sdef.DisableMysqlX {
			log.Debug().Msg("Setting mysqlx=OFF")
			sdef.MyCnfOptions = append(sdef.MyCnfOptions, "mysqlx=OFF")
		} else {
			mysqlx_port := sdef.MysqlXPort
			if mysqlx_port == 0 {
				mysqlx_port, _ = getFreePort()
			}
			sdef.MyCnfOptions = append(sdef.MyCnfOptions, fmt.Sprintf("mysqlx-port=%d", mysqlx_port))
			sdef.MyCnfOptions = append(sdef.MyCnfOptions, fmt.Sprintf("mysqlx-socket=%s/mysqlx-%d.sock", os.TempDir(), mysqlx_port))
			sdef.MorePorts = append(sdef.MorePorts, mysqlx_port)
		}
	}
	if sdef.NodeNum == 0 && !sdef.Force {
		log.Debug().Msg("Trying to get an free/open port")
		if sdef.Port, err = getFreePort(); err != nil {
			return errors.Wrap(err, "Cannot get a free/open port for MySQL")
		}
		log.Info().Msgf("Using port %d for the sandbox", sdef.Port)
	}

	timestamp := time.Now()
	data := map[string]interface{}{
		"Basedir":         sdef.Basedir,
		"Copyright":       SingleTemplates["Copyright"].Contents,
		"DateTime":        timestamp.Format(time.UnixDate),
		"SandboxDir":      sdef.SandboxDir,
		"CustomMysqld":    sdef.CustomMysqld,
		"Port":            sdef.Port,
		"BasePort":        sdef.BasePort,
		"Prompt":          sdef.Prompt,
		"Version":         sdef.Version,
		"Datadir":         datadir,
		"Tmpdir":          tmpdir,
		"GlobalTmpDir":    os.TempDir(),
		"DbUser":          sdef.DbUser,
		"DbPassword":      sdef.DbPassword,
		"RplUser":         sdef.RplUser,
		"RplPassword":     sdef.RplPassword,
		"RemoteAccess":    sdef.RemoteAccess,
		"BindAddress":     sdef.BindAddress,
		"OsUser":          os.Getenv("USER"),
		"ReplOptions":     sdef.ReplOptions,
		"GtidOptions":     sdef.GtidOptions,
		"SemiSyncOptions": sdef.SemiSyncOptions,
		"ExtraOptions":    strings.Join(sdef.MyCnfOptions, "\n"),
		"ReportHost":      fmt.Sprintf("report-host=single-%d", sdef.Port),
		"ReportPort":      fmt.Sprintf("report-port=%d", sdef.Port),
	}
	if sdef.NodeNum != 0 {
		data["ReportHost"] = fmt.Sprintf("report-host = node-%d", sdef.NodeNum)
	}
	if sdef.SkipReportHost || sdef.SBType == "group-node" {
		data["ReportHost"] = ""
	}
	if sdef.SkipReportPort {
		data["ReportPort"] = ""
	}
	if sdef.ServerId > 0 {
		data["ServerId"] = fmt.Sprintf("server-id=%d", sdef.ServerId)
	} else {
		data["ServerId"] = ""
	}

	log.Info().Msgf("Creating sandbox directory %q", sdef.SandboxDir)
	os.MkdirAll(sdef.SandboxDir, os.ModePerm)
	log.Info().Msgf("Creating data directory %q", datadir)
	os.MkdirAll(datadir, os.ModePerm)
	log.Info().Msgf("Creating temporary directory %s", tmpdir)
	os.MkdirAll(tmpdir, os.ModePerm)

	script := path.Join(sdef.Basedir, "scripts", "mysql_install_db")
	init_script_flags := ""
	if !mysqldVersion.LessThan(v570) {
		script = path.Join(sdef.Basedir, "bin", "mysqld")
		init_script_flags = "--initialize-insecure"
	}

	if _, err := exec.LookPath(script); err != nil {
		return fmt.Errorf("Script '%s' not found\n", script)
	}

	if len(sdef.InitOptions) > 0 {
		for _, op := range sdef.InitOptions {
			init_script_flags += " " + op
		}
	}
	data["InitScript"] = script
	data["InitDefaults"] = "--no-defaults"
	if init_script_flags != "" {
		init_script_flags = fmt.Sprintf("\\\n    %s", init_script_flags)
	}
	data["ExtraInitFlags"] = init_script_flags
	data["FixUuidFile1"] = ""
	data["FixUuidFile2"] = ""

	if !sdef.KeepUuid {
		uuid_fname, new_uuid := FixServerUuid(sdef)
		if uuid_fname != "" {
			data["FixUuidFile1"] = fmt.Sprintf(`echo "[auto]" > %s`, uuid_fname)
			data["FixUuidFile2"] = fmt.Sprintf(`echo "%s" >> %s`, new_uuid, uuid_fname)
		}
	}

	log.Debug().Msgf("Writing %s", path.Join(sdef.SandboxDir, "init_db"))
	write_script(SingleTemplates, "init_db", "init_db_template", sdef.SandboxDir, data, true)
	startCmd := path.Join(sdef.SandboxDir, "init_db")
	log.Info().Msgf("Starting the sandbox %s", startCmd)
	cmd := exec.Command(startCmd)
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("Cannot initialize database: %s", err)
	}

	log.Info().Msg("Starting the sandbox")
	if sdef.SBType == "" {
		sdef.SBType = "single"
	}

	write_script(SingleTemplates, "start", "start_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "status", "status_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "stop", "stop_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "clear", "clear_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "use", "use_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "send_kill", "send_kill_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "restart", "restart_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "load_grants", "load_grants_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "add_option", "add_option_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "my", "my_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "show_binlog", "show_binlog_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "show_relaylog", "show_relaylog_template", sdef.SandboxDir, data, true)
	write_script(SingleTemplates, "test_sb", "test_sb_template", sdef.SandboxDir, data, true)

	write_script(SingleTemplates, "my.sandbox.cnf", "my_cnf_template", sdef.SandboxDir, data, false)
	switch {
	case !mysqldVersion.LessThan(v800):
		write_script(SingleTemplates, "grants.mysql", "grants_template8x", sdef.SandboxDir, data, false)
	case !mysqldVersion.LessThan(v576):
		write_script(SingleTemplates, "grants.mysql", "grants_template57", sdef.SandboxDir, data, false)
	default:
		write_script(SingleTemplates, "grants.mysql", "grants_template5x", sdef.SandboxDir, data, false)
	}
	write_script(SingleTemplates, "sb_include", "sb_include_template", sdef.SandboxDir, data, false)

	preGrantSQLFile := path.Join(sdef.SandboxDir, "pre_grants.sql")
	postGrantSQLFile := path.Join(sdef.SandboxDir, "post_grants.sql")

	cmds := [][]string{}
	cmds = append(cmds, []string{path.Join(sdef.SandboxDir, "start")})

	if sdef.PreGrantsSqlFile != "" {
		util.CopyFile(sdef.PreGrantsSqlFile, preGrantSQLFile)
		cmds = append(cmds, []string{path.Join(sdef.SandboxDir, "load_grants"), "pre_grants.sql"})
	}

	cmds = append(cmds, []string{path.Join(sdef.SandboxDir, "load_grants")})
	if sdef.PostGrantsSqlFile != "" {
		util.CopyFile(sdef.PostGrantsSqlFile, postGrantSQLFile)
		cmds = append(cmds, []string{path.Join(sdef.SandboxDir, "load_grants"), "post_grants.sql"})
	}

	if len(sdef.PreGrantsSql) > 0 {
		util.AppendStrings(sdef.PreGrantsSql, preGrantSQLFile, ";")
	}
	if len(sdef.PostGrantsSql) > 0 {
		util.AppendStrings(sdef.PostGrantsSql, postGrantSQLFile, ";")
	}

	if !sdef.SkipStart {
		for _, args := range cmds {
			cmd := exec.Command(args[0], args[1:]...)
			log.Info().Msgf("Running command %s", strings.Join(args, " "))
			if b, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("Cannot run %s %v\n%s\n", cmd.Path, cmd.Args, string(b))
			}
		}
	}

	log.Info().Msg("Sandbox started")
	return nil
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

func write_script(temp_var TemplateCollection, name, template_name, directory string, data map[string]interface{}, make_executable bool) error {
	tmpl := strings.TrimSpace(temp_var[template_name].Contents)
	data["TemplateName"] = template_name
	timestamp := time.Now()
	_, time_stamp_exists := data["DateTime"]
	if !time_stamp_exists {
		data["DateTime"] = timestamp.Format(time.UnixDate)
	}
	t := template.Must(template.New("tmp").Parse(tmpl))
	buf := &bytes.Buffer{}

	if err := t.Execute(buf, data); err != nil {
		return errors.Wrapf(err, "Cannot parse template %q", template_name)
	}
	fname := path.Join(directory, name)
	util.AppendStrings([]string{buf.String()}, fname, "")
	if make_executable {
		os.Chmod(fname, 0744)
	}
	return nil
}
