package report

import (
	"bytes"
	"testing"

	tu "github.com/Percona-Lab/pt-mysql-config-diff/testutils"
)

func TestReport(t *testing.T) {
	want := `### Minimum Permissions

    ----------------------------------------------------------------------------------------------------
    Grants : SELECT
    ----------------------------------------------------------------------------------------------------
    Queries: SELECT a FROM t1
             SELECT b FROM t2

          `
	rg := map[string][]string{
		"SELECT": {"SELECT a FROM t1", "SELECT b FROM t2"},
	}

	buf := new(bytes.Buffer)
	PrintReport(rg, buf)

	tu.Assert(t, buf.String() != want, "Invalid report output")
}
