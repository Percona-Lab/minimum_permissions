package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	"github.com/alecthomas/kingpin"
	"github.com/fighterlyt/permutation"
	_ "github.com/go-sql-driver/mysql"
	log "github.com/sirupsen/logrus"
)

var (
	app = kingpin.New("mysql_random_data_loader", "MySQL Random Data Loader")

	host          = app.Flag("host", "Host name/IP").Short('h').Default("127.0.0.1").String()
	pass          = app.Flag("password", "Password").Short('p').String()
	port          = app.Flag("port", "Port").Short('P').Default("3306").Int()
	prepareFile   = app.Flag("prepare-file", "File having queries to run before start").String()
	user          = app.Flag("user", "User").Short('u').String()
	testStatement = app.Flag("test-statement", "Query to test").Required().String()
	verbose       = app.Flag("verbose", "Show all permissions being tested").Bool()
	version       = app.Flag("version", "Show version and exit").Bool()

	Version   = "0.0.0."
	Commit    = "<sha1>"
	Branch    = "branch-name"
	Build     = "2017-01-01"
	GoVersion = "1.9.2"
)

func main() {
	_, err := app.Parse(os.Args[1:])

	if *version {
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
		log.SetLevel(log.InfoLevel)
	}

	protocol := "tcp"
	hostPort := *host

	if *host == "localhost" {
		protocol = "unix"
	} else {
		hostPort = fmt.Sprintf("%s:%d", *host, *port)
	}

	dsn := fmt.Sprintf("%s:%s@%s(%s)/?multiStatements=true", *user, *pass, protocol, hostPort)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Cannot connect to the db using %q: %s", dsn, err)
	}

	if *prepareFile != "" {
		if _, err = os.Stat(*prepareFile); err == nil {
			cmds, err := ioutil.ReadFile(*prepareFile)
			if err != nil {
				log.Fatalf("Cannot open %q: %s", *prepareFile, err)
			}
			_, err = db.Exec(string(cmds))
			if err != nil {
				log.Fatalf("Cannot run prepare queries: %s", err)
			}
		}
	}

	query := "GRANT ALL PRIVILEGES ON *.* TO `testuser`@`%s` IDENTIFIED BY 'testpwd'"
	_, err = db.Exec(fmt.Sprintf(query, *host))
	if err != nil {
		log.Fatalf("Cannot grant all privileges to the test user.\n%s\n%s", query, err)
	}

	query = fmt.Sprintf("SHOW GRANTS FOR `testuser`@`%s`", *host)
	rows, err := db.Query(query)
	if err != nil {
		log.Fatalf("Cannot get grants for the test user.\n%s\n%s", query, err)
	}

	re := regexp.MustCompile("^GRANT (.*?) ON .*$")
	userGrants := []string{}

	for rows.Next() {
		var grantsCmd string

		err := rows.Scan(&grantsCmd)
		if err != nil {
			log.Fatalf("Cannot read grants: %s", err)
		}

		m := re.FindAllStringSubmatch(grantsCmd, -1)
		if len(m) < 1 {
			continue
		}
		grants := strings.Split(m[0][1], ",")
		userGrants = append(userGrants, grants...)
	}

	log.Infof("ALL GRANTS: %s\n", strings.Join(userGrants, ", "))

	revokeQuery := fmt.Sprintf("REVOKE ALL PRIVILEGES ON *.* FROM `testuser`@`%s`", *host)

	dsn2 := fmt.Sprintf("testuser:testpwd@%s(%s)/", protocol, hostPort)
	minimumWorkingGrants := getMinimumWorkingGrants(db, dsn2, userGrants, *testStatement, revokeQuery)
	minimumGrants := []string{}

	p, err := permutation.NewPerm(minimumWorkingGrants, less)
	for i, err := p.Next(); err == nil; i, err = p.Next() {
		grants := getMinimumWorkingGrants(db, dsn2, i.([]string), *testStatement, revokeQuery)
		if len(minimumGrants) == 0 || len(grants) < len(minimumGrants) {
			minimumGrants = grants
		}
	}

	query = fmt.Sprintf("GRANT %s ON *.* TO `testuser`@`%s` IDENTIFIED BY 'testpwd'", strings.Join(minimumGrants, ", "), *host)
	fmt.Printf("\nMinimum working permissions:\n%s\n", query)
}

func less(i, j interface{}) bool {
	if i.(string) < j.(string) {
		return true
	}
	return false
}

func getMinimumWorkingGrants(db *sql.DB, dsn2 string, grantsList []string, testQuery, revokeQuery string) []string {
	minimumWorkingGrants := []string{}

	_, err := db.Exec(revokeQuery)
	if err != nil {
		log.Fatalf("Cannot revoke all privileges for the test user.\n%s\n%s", revokeQuery, err)
	}

	for i := 0; i < len(grantsList); i++ {
		grants := strings.Join(grantsList[:i+1], ",")
		query := fmt.Sprintf("GRANT %s ON *.* TO `testuser`@`%s` IDENTIFIED BY 'testpwd'", grants, *host)

		log.Infoln("====================================================================================================")
		log.Infoln(query)

		_, err := db.Exec(query)
		if err != nil {
			log.Fatalf("\n\nCannot GRANT privileges: %q:\n%s", query, err)
		}
		db.Exec("FLUSH PRIVILEGES")

		db2, err := sql.Open("mysql", dsn2)
		if err != nil {
			panic(err)
		}
		_, err = db2.Query(testQuery)
		db2.Close()

		if err == nil {
			log.Infoln("OK")
			minimumWorkingGrants = grantsList[:i+1]
			break
		}

		log.Infoln(err)
	}

	return minimumWorkingGrants

}
