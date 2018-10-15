package qreader

import (
	"bufio"
	"os"
	"regexp"
	"strings"

	"github.com/Percona-Lab/minimum_permissions/internal/tester"
	"github.com/Percona-Lab/minimum_permissions/internal/utils"
	slo "github.com/percona/go-mysql/log"
	"github.com/percona/go-mysql/log/slow"
	"github.com/percona/go-mysql/query"
	"github.com/pkg/errors"
)

// ReadSlowLog read and parse a slow log file and returns a list of testing cases
func ReadSlowLog(filename string) ([]*tester.TestingCase, error) {
	filename = utils.ExpandHomeDir(filename)
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	slp := slow.NewSlowLogParser(file, slo.Options{Debug: false})

	go slp.Start()

	queryGroups := make(map[string]*slo.Event)

	for e := range slp.EventChan() {
		fp := query.Fingerprint(e.Query)
		queryGroups[fp] = e
	}

	testCases := []*tester.TestingCase{}
	for fingerprint, event := range queryGroups {
		testCases = append(testCases, &tester.TestingCase{Database: event.Db, Query: event.Query, Fingerprint: fingerprint})
	}
	slp.Stop()

	return testCases, nil
}

// ReadSlowLog read and parse a plain file where ALL the queries return with a semicolon
// and returns a list of testing cases
func ReadPlainFile(filename string) ([]*tester.TestingCase, error) {
	filename = utils.ExpandHomeDir(filename)
	file, err := os.Open(filename)
	if err != nil {
		return nil, errors.Wrapf(err, "Cannot open %s", filename)
	}
	defer file.Close()

	tc := []*tester.TestingCase{}
	lines := []string{}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	queries := joinQueryLines(lines)

	for _, query := range queries {
		tc = append(tc, &tester.TestingCase{Query: query})
	}

	return tc, nil
}

func ReadGeneralLog(filename string) ([]*tester.TestingCase, error) {
	exp := `(?s)\A` +
		`(?:(\d{6}\s+\d{1,2}:\d\d:\d\d|\d{4}-\d{1,2}-\d{1,2}T\d\d:\d\d:\d\d\.\d+(?:Z|-?\d\d:\d\d)?))?` + // # Timestamp
		`\s+` +
		`(?:\s*(\d+))` + //                     # Thread ID
		`\s` +
		`(\w+)` + //                            # Command
		`\s+` +
		`(.*)` //                             # Argument
	re := regexp.MustCompile(exp)

	filename = utils.ExpandHomeDir(filename)
	file, err := os.Open(filename)
	if err != nil {
		return nil, errors.Wrapf(err, "Cannot open %s", filename)
	}
	defer file.Close()

	tc := []*tester.TestingCase{}
	query := ""
	inAdminCmd := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		m := re.FindStringSubmatch(line)
		if len(m) > 3 {
			// we found a new query, that signals we already parsed the previous query.
			// append the previous one to the tests cases slice
			if query != "" {
				tc = append(tc, &tester.TestingCase{Query: query})
				query = ""
			}

			if m[3] == "Query" {
				query = m[4]
				inAdminCmd = false
				continue
			}

			inAdminCmd = true
		} else {
			if inAdminCmd {
				continue
			}
			query += line
		}
	}
	if query != "" {
		tc = append(tc, &tester.TestingCase{Query: query})
	}

	return tc, nil
}

func joinQueryLines(lines []string) []string {
	inQuery := false
	joined := []string{}
	queryString := ""
	separator := ""

	for _, line := range lines {
		if !inQuery {
			inQuery = true
		}
		if inQuery {
			queryString += separator + line
			separator = "\n"
			if !strings.HasSuffix(strings.TrimSpace(line), ";") {
				continue
			}
			inQuery = false
			separator = ""
			joined = append(joined, queryString)
			queryString = ""
			continue
		}
		joined = append(joined, line)
	}
	return joined
}
