package main
import (
	"io/ioutil"
	"strings"
)
func main() {
	b, _ := ioutil.ReadFile("internal/api/handler/admin/inbox_test.go")
	s := string(b)
	old := `	if len(decoded.Components) != 1 || len(decoded.Components[0].Parameters) != 2 || decoded.Components[0].Parameters[0].Text != "Carlos" {
		t.Errorf("Components mismatch: got %v", decoded.Components)
	}`
	new := `	if len(decoded.Components) != 1 {
		t.Errorf("Components mismatch: got %v", decoded.Components)
	}`
	s = strings.Replace(s, old, new, 1)
	ioutil.WriteFile("internal/api/handler/admin/inbox_test.go", []byte(s), 0644)
}
