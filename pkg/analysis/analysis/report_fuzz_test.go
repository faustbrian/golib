package analysis_test

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
)

func FuzzReportWriters(f *testing.F) {
	f.Add("security/no-unsafe", "internal/unsafe.go", "unsafe import")
	f.Add("rule\"</script>", "name with spaces.go", "message\nwith controls\x00")
	f.Add("", "../escape.go", "invalid path")

	f.Fuzz(func(t *testing.T, rule, filename, message string) {
		if len(rule)+len(filename)+len(message) > 64<<10 {
			t.Skip()
		}
		report := shared.Report{
			ToolVersion: message,
			Rules: []shared.Rule{{
				ID:          rule,
				Rationale:   message,
				Remediation: message,
			}},
			Diagnostics: []shared.Diagnostic{{
				Rule:     rule,
				Filename: filename,
				Line:     1,
				Column:   1,
				Message:  message,
			}},
			Suppressions: []shared.Suppression{{
				Rule:          rule,
				Filename:      filename,
				DirectiveLine: 1,
				TargetLine:    1,
				Reason:        message,
			}},
		}
		for name, write := range map[string]func(io.Writer, shared.Report) error{
			"JSON":  shared.WriteJSON,
			"SARIF": shared.WriteSARIF,
		} {
			var output bytes.Buffer
			if err := write(&output, report); err != nil {
				continue
			}
			if !json.Valid(output.Bytes()) {
				t.Fatalf("%s writer emitted invalid JSON", name)
			}
			if bytes.Contains(output.Bytes(), []byte(`"snippet"`)) ||
				bytes.Contains(output.Bytes(), []byte(`"source"`)) {
				t.Fatalf("%s writer emitted source-bearing fields", name)
			}
		}
	})
}
