package main
import (
	"os/exec"
)
func main() {
	exec.Command("goimports", "-w", "internal/channel/whatsapp/waba.go").Run()
}
