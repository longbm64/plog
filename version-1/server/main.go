package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/DangLong/na-server-go/pkg/cli"
	"github.com/DangLong/na-server-go/pkg/daemon"
)

func init() {
	if runtime.GOOS == "linux" {
		os.Setenv("LANG", "C.UTF-8")
		os.Setenv("LC_ALL", "C.UTF-8")
	}
}

func requireAdmin() {
	if runtime.GOOS != "windows" && os.Getuid() != 0 {
		// Try sudo first, fallback message if sudo not available
		sudoPath, err := exec.LookPath("sudo")
		if err == nil {
			fmt.Println("🔐 HE THONG YEU CAU QUYEN ROOT DE TIEP TUC.")
			cmd := exec.Command(sudoPath, append([]string{os.Args[0]}, os.Args[1:]...)...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
			os.Exit(0)
		}
		fmt.Println("🔐 HE THONG YEU CAU QUYEN ROOT DE TIEP TUC.")
		fmt.Println("   Vui long chay lenh: su -c nalink")
		os.Exit(1)
	}
}

func main() {
	requireAdmin()

	if len(os.Args) >= 2 && os.Args[1] == "daemon" {
		port := "3001"
		token := ""
		tokenFile := ""
		for _, arg := range os.Args[2:] {
			if strings.HasPrefix(arg, "--port=") {
				port = strings.TrimPrefix(arg, "--port=")
			}
			if strings.HasPrefix(arg, "--token=") {
				token = strings.TrimPrefix(arg, "--token=")
			}
			if strings.HasPrefix(arg, "--token-file=") {
				tokenFile = strings.TrimPrefix(arg, "--token-file=")
			}
		}
		// BẢO MẬT C3: Ưu tiên đọc token từ file
		if tokenFile != "" {
			data, err := os.ReadFile(tokenFile)
			if err != nil {
				fmt.Printf("Loi doc token file %s: %v\n", tokenFile, err)
				os.Exit(1)
			}
			token = strings.TrimSpace(string(data))
		}
		daemon.StartDaemon(port, token)
		return
	}

	// Check --auto flag for unattended remote installation
	for _, arg := range os.Args[1:] {
		if arg == "--auto" {
			cli.ShowMenuAuto()
			return
		}
	}

	// Show main interactive menu
	cli.ShowMenu()
}
