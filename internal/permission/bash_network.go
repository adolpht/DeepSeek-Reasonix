package permission

import "strings"

// networkCommandPrefixes lists command names that require network access.
var networkCommandPrefixes = []string{
	"curl", "wget", "ssh", "scp", "rsync",
	"ping", "dig", "nslookup", "nc", "ncat",
	"telnet", "ftp", "sftp", "aria2c",
	"docker", "kubectl",
}

// BashRequiresNetwork reports whether a bash command likely requires
// network access, based on a simple prefix/pattern heuristic.
func BashRequiresNetwork(command string) bool {
	// Get the first word (the command name)
	cmd := firstCommandWord(command)
	for _, prefix := range networkCommandPrefixes {
		if cmd == prefix {
			return true
		}
	}
	// Check for pipe into network commands
	if strings.Contains(command, "|") {
		parts := strings.Split(command, "|")
		for _, part := range parts {
			if BashRequiresNetwork(strings.TrimSpace(part)) {
				return true
			}
		}
	}
	return false
}

// firstCommandWord extracts the command name from a shell command string.
func firstCommandWord(command string) string {
	command = strings.TrimSpace(command)
	// Handle common prefixes
	for _, prefix := range []string{"sudo ", "time ", "nice ", "ionice ", "env "} {
		if strings.HasPrefix(command, prefix) {
			command = strings.TrimSpace(strings.TrimPrefix(command, prefix))
		}
	}
	// Get the first word
	if i := strings.IndexAny(command, " \t\n;&|<>()"); i > 0 {
		command = command[:i]
	}
	// Strip path
	if i := strings.LastIndex(command, "/"); i >= 0 {
		command = command[i+1:]
	}
	return command
}
