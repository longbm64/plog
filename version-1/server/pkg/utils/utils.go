package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ShowSpinner hien thi hieu ung dau cham chay trong khi doi command
func ShowSpinner(msg string) func() {
	stop := make(chan bool)
	go func() {
		frames := []string{"   ", ".  ", ".. ", "..."}
		i := 0
		for {
			select {
			case <-stop:
				fmt.Printf("\r   - %s... [OK]\n", msg)
				return
			default:
				fmt.Printf("\r   - %s%s  ", msg, frames[i%len(frames)])
				i++
				time.Sleep(400 * time.Millisecond)
			}
		}
	}()
	return func() {
		stop <- true
	}
}

// GenerateSecureToken creates a random hex string of the specified byte length
func GenerateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

var (
	usedPorts = make(map[int]bool)
	portMutex sync.Mutex
)

// GetFreePort returns an available TCP port
func GetFreePort() (int, error) {
	portMutex.Lock()
	defer portMutex.Unlock()

	for {
		addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
		if err != nil {
			return 0, err
		}

		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return 0, err
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()

		if !usedPorts[port] {
			usedPorts[port] = true
			return port, nil
		}
	}
}

// GetPublicIP retrieves the VPS public IP address using an external service
func GetPublicIP() string {
	cmd := exec.Command("curl", "-s", "ifconfig.me")
	out, err := cmd.Output()
	if err != nil {
		return "Khong lay duoc"
	}
	return strings.TrimSpace(string(out))
}

// ReadFileContent returns the content of a file, or empty string if error
func ReadFileContent(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// ExecCmd runs a command and returns its standard output as a string.
func ExecCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
