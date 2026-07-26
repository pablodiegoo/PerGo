package main
import (
	"io/ioutil"
	"strings"
)
func main() {
	b, _ := ioutil.ReadFile("internal/channel/whatsapp/waba.go")
	s := string(b)
	s = strings.Replace(s, `if len(comp.Parameters) > 0 {`, `if params, ok := comp.Parameters.([]domain.TemplateParameter); ok && len(params) > 0 {`, 1)
	s = strings.Replace(s, `tmpl.Components[i].Parameters = make([]wabaParameter, len(comp.Parameters))`, `tmpl.Components[i].Parameters = make([]wabaParameter, len(params))`, 1)
	s = strings.Replace(s, `for j, param := range comp.Parameters {`, `for j, param := range params {`, 1)
	ioutil.WriteFile("internal/channel/whatsapp/waba.go", []byte(s), 0644)
}
