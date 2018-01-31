package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	"github.com/alecthomas/kingpin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var (
	app = kingpin.New("mysql_random_data_loader", "MySQL Random Data Loader")

	host          = app.Flag("host", "Host name/IP").Short('h').String()
	maxDepth      = app.Flag("max-depth", "Maximum number of permissions to try").Default("10").Int()
	pass          = app.Flag("password", "Password").Short('p').String()
	port          = app.Flag("port", "Port").Short('P').Int()
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

	userGrants, err := getAllGrants(db)
	if err != nil {
		log.Fatalf("Cannot get user grants: %s", err)
	}

	log.Infof("ALL GRANTS: %s\n", strings.Join(userGrants, ", "))

	dsn2 := fmt.Sprintf("testuser:testpwd@%s(%s)/", protocol, hostPort)
	revokeQuery := fmt.Sprintf("REVOKE ALL PRIVILEGES ON *.* FROM `testuser`@`%s`", *host)
	minimumGrants := []string{}

	for i := 1; i < *maxDepth; i++ {
		grantsCombinations := getGrantsCombinations(userGrants, i)
		minimumGrants, err = getMinimumWorkingGrants(db, dsn2, grantsCombinations, *testStatement, revokeQuery)
		if err != nil {
			log.Fatal(err)
		}
		if len(minimumGrants) > 0 {
			break
		}
	}

	if len(minimumGrants) == 0 {
		fmt.Println("Cannot get minimum set of permissions")
		return
	}

	query := fmt.Sprintf("GRANT %s ON *.* TO `testuser`@`%s` IDENTIFIED BY 'testpwd'", strings.Join(minimumGrants, ", "), *host)
	fmt.Printf("\nMinimum working permissions:\n%s\n", query)

	log.Debugf("Dropping test user")
	query = fmt.Sprintf("DROP USER IF EXISTS `testuser`@`%s`", *host)
	if _, err := db.Exec(query); err != nil {
		log.Fatalf("Cannot drop test user:\n%s\n%s", query, err)
	}
}

func less(i, j interface{}) bool {
	if i.(string) < j.(string) {
		return true
	}
	return false
}

func getMinimumWorkingGrants(db *sql.DB, dsn2 string, grantsList [][]string, testQuery, revokeQuery string) ([]string, error) {
	minimumWorkingGrants := []string{}

	for i := 0; i < len(grantsList); i++ {
		_, err := db.Exec(revokeQuery)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot revoke all privileges for the test user.\n%s", revokeQuery)
		}
		grants := strings.Join(grantsList[i], ",")
		query := fmt.Sprintf("GRANT %s ON *.* TO `testuser`@`%s` IDENTIFIED BY 'testpwd'", grants, *host)

		log.Infoln("====================================================================================================")
		log.Infoln(query)

		_, err = db.Exec(query)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot GRANT privileges: %q", query)
		}
		db.Exec("FLUSH PRIVILEGES")

		db2, err := sql.Open("mysql", dsn2)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot connect to the db using %s", dsn2)
		}
		_, err = db2.Query(testQuery)
		db2.Close()

		if err == nil {
			log.Infoln("OK")
			minimumWorkingGrants = grantsList[i]
			break
		}

		log.Infoln(err)
	}

	return minimumWorkingGrants, nil

}

func getGrantsCombinations(grants []string, length int) [][]string {
	grantsArray := [][]string{}

	combinations := comb(len(grants), length)

	for _, combRow := range combinations {
		grantsList := []string{}
		for _, grant := range combRow {
			grantsList = append(grantsList, grants[grant])
		}
		grantsArray = append(grantsArray, grantsList)
	}
	return grantsArray
}

func comb(n, m int) [][]int {
	s := make([]int, m)
	combinations := [][]int{}

	last := m - 1
	var rc func(int, int)
	rc = func(i, next int) {
		for j := next; j < n; j++ {
			s[i] = j
			if i == last {
				ss := make([]int, len(s))
				copy(ss, s)
				combinations = append(combinations, ss)
			} else {
				rc(i+1, j+1)
			}
		}
		// return
	}
	rc(0, 0)

	return combinations
}

func getAllGrants(db *sql.DB) ([]string, error) {
	re := regexp.MustCompile("^GRANT (.*?) ON .*$")
	userGrants := []string{}

	query := "GRANT ALL PRIVILEGES ON *.* TO `testuser`@`%s` IDENTIFIED BY 'testpwd'"
	if _, err := db.Exec(fmt.Sprintf(query, *host)); err != nil {
		return nil, err
	}

	query = fmt.Sprintf("SHOW GRANTS FOR `testuser`@`%s`", *host)
	rows, err := db.Query(query)
	if err != nil {
		return nil, errors.Wrapf(err, "Cannot get grants for the test user.\n%s", query)
	}

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
	for i := 0; i < len(userGrants); i++ {
		userGrants[i] = strings.TrimSpace(userGrants[i])
	}

	return userGrants, nil
}
