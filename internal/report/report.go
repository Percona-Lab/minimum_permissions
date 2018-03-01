package report

import (
	"io"
	"strings"
	"text/template"

	"github.com/Percona-Lab/minimum_permissions/internal/tester"
)

func GroupResults(results []*tester.TestingCase) map[string][]string {
	rg := map[string][]string{}

	for _, res := range results {
		key := strings.Join(res.MinimumGrants, ", ")
		if _, ok := rg[key]; ok {
			rg[key] = append(rg[key], res.Query)
			continue
		}
		rg[key] = []string{res.Query}
	}

	return rg
}

func stripCtlFromUTF8(str string) string {
	return strings.Map(func(r rune) rune {
		if r >= 32 && r != 127 {
			return r
		}
		return -1
	}, str)
}

func PrintReport(rg map[string][]string, w io.Writer) error {
	report := `### Minimum Permissions
{{ range $index, $element := . }}
----------------------------------------------------------------------------------------------------
Grants : {{ $index }}
----------------------------------------------------------------------------------------------------
{{- range $i, $q := . }}
{{ . -}} 
{{end}} 

{{ end}}
`

	t := template.Must(template.New("report").Parse(report))
	err := t.Execute(w, rg)
	return err
}
