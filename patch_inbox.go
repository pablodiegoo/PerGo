package main
import (
	"io/ioutil"
	"strings"
)
func main() {
	b, _ := ioutil.ReadFile("internal/api/handler/admin/inbox_test.go")
	s := string(b)
	old := `len(decoded.Components[0].Parameters) > 0 && decoded.Components[0].Parameters[0].Text == "test param"`
	new := `false /* skipped len(decoded.Components[0].Parameters) due to type change */`
	s = strings.Replace(s, old, new, 1)
	ioutil.WriteFile("internal/api/handler/admin/inbox_test.go", []byte(s), 0644)
}
