package utils

import (
	"os"
	"strings"
)

// IsDebian returns true if the OS is Debian-based (Ubuntu, Debian)
func IsDebian() bool {
	content, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(content))
	return strings.Contains(lower, "ubuntu") || strings.Contains(lower, "debian")
}

// IsRHEL returns true if the OS is RHEL-based (CentOS, Rocky, AlmaLinux, Oracle, Fedora, RHEL)
func IsRHEL() bool {
	content, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(content))
	return strings.Contains(lower, "centos") ||
		strings.Contains(lower, "rocky") ||
		strings.Contains(lower, "almalinux") ||
		strings.Contains(lower, "oracle") ||
		strings.Contains(lower, "fedora") ||
		strings.Contains(lower, "rhel") ||
		strings.Contains(lower, "red hat")
}

// GetPkgManager returns "apt", "dnf", or "yum" based on the OS
func GetPkgManager() string {
	if IsDebian() {
		return "apt"
	}
	// Prefer dnf over yum (CentOS 8+, Rocky, Alma, Fedora)
	if _, err := os.Stat("/usr/bin/dnf"); err == nil {
		return "dnf"
	}
	return "yum"
}

// GetLogPath returns the auth log path based on the OS
func GetLogPath() string {
	if IsDebian() {
		return "/var/log/auth.log"
	}
	return "/var/log/secure"
}
