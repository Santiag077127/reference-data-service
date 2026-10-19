package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
)

// repositoryErrorObservation intentionally contains no error message, query,
// query parameter, connection detail, or request data.
type repositoryErrorObservation struct {
	operation        string
	category         string
	errorType        string
	contextCanceled  bool
	contextDeadline  bool
	connectionDone   bool
	postgresCode     string
	connectCauseType string
	netCauseType     string
	netTimeout       bool
	netTemporary     bool
	errnoClass       string
}

func observeRepositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	observation := classifyRepositoryError(operation, err)
	log.Printf(
		"repository_error operation=%s category=%s error_type=%s connect_cause_type=%s net_cause_type=%s net_timeout=%t net_temporary=%t errno_class=%s context_canceled=%t context_deadline_exceeded=%t sql_err_conn_done=%t postgres_code=%s",
		observation.operation,
		observation.category,
		observation.errorType,
		observation.connectCauseType,
		observation.netCauseType,
		observation.netTimeout,
		observation.netTemporary,
		observation.errnoClass,
		observation.contextCanceled,
		observation.contextDeadline,
		observation.connectionDone,
		observation.postgresCode,
	)
	return err
}

func classifyRepositoryError(operation string, err error) repositoryErrorObservation {
	observation := repositoryErrorObservation{
		operation:        operation,
		category:         "other",
		errorType:        fmt.Sprintf("%T", err),
		contextCanceled:  errors.Is(err, context.Canceled),
		contextDeadline:  errors.Is(err, context.DeadlineExceeded),
		connectionDone:   errors.Is(err, sql.ErrConnDone),
		postgresCode:     "none",
		connectCauseType: "none",
		netCauseType:     "none",
		errnoClass:       "UNKNOWN",
	}
	var connectError *pgconn.ConnectError
	if errors.As(err, &connectError) {
		observation.category = "connect_other"
		observation.connectCauseType = connectErrorCauseType(connectError)
		observation.netCauseType, observation.netTimeout, observation.netTemporary = netOperationDetails(err)
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError != nil && postgresError.Code != "" {
		observation.category = "postgres"
		observation.postgresCode = postgresError.Code
	}
	if connectError != nil && observation.category == "connect_other" {
		if category, errnoClass, ok := classifyConnectionErrno(err); ok {
			observation.category = category
			observation.errnoClass = errnoClass
		}
	}
	switch {
	case observation.contextCanceled:
		observation.category = "context_canceled"
	case observation.contextDeadline:
		observation.category = "context_deadline_exceeded"
	case observation.connectionDone:
		observation.category = "sql_connection_done"
	case connectError != nil && observation.category == "connect_other" && isDNSError(err):
		observation.category = "dns_error"
	case connectError != nil && observation.category == "connect_other" && isNetworkTimeout(err):
		observation.category = "connection_timeout"
	}
	return observation
}

func connectErrorCauseType(err *pgconn.ConnectError) string {
	if cause := errors.Unwrap(err); cause != nil {
		var dnsError *net.DNSError
		if errors.As(cause, &dnsError) {
			return fmt.Sprintf("%T", dnsError)
		}
		var operationError *net.OpError
		if errors.As(cause, &operationError) {
			return fmt.Sprintf("%T", operationError)
		}
		var postgresError *pgconn.PgError
		if errors.As(cause, &postgresError) {
			return fmt.Sprintf("%T", postgresError)
		}
		return fmt.Sprintf("%T", cause)
	}
	return "none"
}

func isDNSError(err error) bool {
	var dnsError *net.DNSError
	return errors.As(err, &dnsError)
}

func isNetworkTimeout(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

func netOperationDetails(err error) (causeType string, timeout, temporary bool) {
	var operationError *net.OpError
	if !errors.As(err, &operationError) || operationError == nil || operationError.Err == nil {
		return "none", false, false
	}
	return fmt.Sprintf("%T", operationError.Err), operationError.Timeout(), operationError.Temporary()
}

// classifyConnectionErrno returns only closed, non-sensitive labels. It never
// exposes the numeric errno, syscall name, network operation, or address.
func classifyConnectionErrno(err error) (category, errnoClass string, ok bool) {
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused", "ECONNREFUSED", true
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset", "ECONNRESET", true
	case errors.Is(err, syscall.ECONNABORTED):
		return "connection_aborted", "ECONNABORTED", true
	case errors.Is(err, syscall.ETIMEDOUT):
		return "connection_timeout", "ETIMEDOUT", true
	}

	var syscallError *os.SyscallError
	if !errors.As(err, &syscallError) || syscallError == nil {
		return "", "UNKNOWN", false
	}
	errno, ok := syscallError.Err.(syscall.Errno)
	if !ok {
		return "", "UNKNOWN", false
	}
	return classifyPlatformConnectionErrno(errno)
}
