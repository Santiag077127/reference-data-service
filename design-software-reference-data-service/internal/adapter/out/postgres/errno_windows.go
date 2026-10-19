//go:build windows

package postgres

import "syscall"

// Windows socket failures use Winsock values. They differ from several
// portability sentinels in syscall, so compare the native values structurally
// and return only a closed classification label.
const (
	windowsWSAConnAborted syscall.Errno = 10053
	windowsWSAConnReset   syscall.Errno = 10054
	windowsWSAConnRefused syscall.Errno = 10061
	windowsWSATimedOut    syscall.Errno = 10060
)

func classifyPlatformConnectionErrno(errno syscall.Errno) (category, errnoClass string, ok bool) {
	switch errno {
	case windowsWSAConnRefused:
		return "connection_refused", "ECONNREFUSED", true
	case windowsWSAConnReset:
		return "connection_reset", "ECONNRESET", true
	case windowsWSAConnAborted:
		return "connection_aborted", "ECONNABORTED", true
	case windowsWSATimedOut:
		return "connection_timeout", "ETIMEDOUT", true
	default:
		return "", "UNKNOWN", false
	}
}
