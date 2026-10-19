package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifyRepositoryError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		category string
		code     string
	}{
		{name: "context canceled", err: context.Canceled, category: "context_canceled", code: "none"},
		{name: "context deadline", err: context.DeadlineExceeded, category: "context_deadline_exceeded", code: "none"},
		{name: "connection done", err: sql.ErrConnDone, category: "sql_connection_done", code: "none"},
		{name: "postgres error", err: &pgconn.PgError{Code: "53300", Message: "must not be logged"}, category: "postgres", code: "53300"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyRepositoryError("ReportActiveCatalog.page", tt.err)
			if got.category != tt.category || got.postgresCode != tt.code {
				t.Fatalf("got category=%q code=%q, want category=%q code=%q", got.category, got.postgresCode, tt.category, tt.code)
			}
		})
	}
}

func TestClassifyConnectErrorWithoutLoggingSensitiveErrorText(t *testing.T) {
	connectError := testConnectError(t, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED})
	observation := classifyRepositoryError("List.read_tx", connectError)
	if observation.category != "connection_refused" {
		t.Fatalf("got category=%q, want connection_refused", observation.category)
	}
	if observation.errnoClass != "ECONNREFUSED" {
		t.Fatalf("got errno_class=%q, want ECONNREFUSED", observation.errnoClass)
	}
	if observation.errorType != "*pgconn.ConnectError" || observation.connectCauseType != "*net.OpError" || observation.netCauseType != "syscall.Errno" {
		t.Fatalf("got error_type=%q cause_type=%q net_cause_type=%q", observation.errorType, observation.connectCauseType, observation.netCauseType)
	}

	var output bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previousWriter) })
	observeRepositoryError("List.read_tx", connectError)
	line := output.String()
	for _, prohibited := range []string{"test-user", "test-database", "test-password", "diagnostic.invalid", "postgres://", "connection refused"} {
		if strings.Contains(line, prohibited) {
			t.Fatalf("log leaked %q: %s", prohibited, line)
		}
	}
	if !strings.Contains(line, "category=connection_refused") || !strings.Contains(line, "connect_cause_type=*net.OpError") || !strings.Contains(line, "net_cause_type=syscall.Errno") {
		t.Fatalf("missing safe connection classification: %s", line)
	}
}

func TestClassifyConnectErrorTimeout(t *testing.T) {
	connectError := testConnectError(t, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ETIMEDOUT})
	observation := classifyRepositoryError("List.read_tx", connectError)
	if observation.category != "connection_timeout" {
		t.Fatalf("got category=%q, want connection_timeout", observation.category)
	}
	if observation.netCauseType != "syscall.Errno" {
		t.Fatalf("got net_cause_type=%q, want syscall.Errno", observation.netCauseType)
	}
	if observation.errnoClass != "ETIMEDOUT" {
		t.Fatalf("got errno_class=%q, want ETIMEDOUT", observation.errnoClass)
	}
}

func TestClassifyWindowsNativeSocketErrnos(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Winsock errno classification")
	}
	tests := []struct {
		name     string
		native   syscall.Errno
		sentinel error
		category string
		class    string
	}{
		{name: "refused", native: windowsWSAConnRefused, sentinel: syscall.ECONNREFUSED, category: "connection_refused", class: "ECONNREFUSED"},
		{name: "reset", native: windowsWSAConnReset, sentinel: syscall.ECONNRESET, category: "connection_reset", class: "ECONNRESET"},
		{name: "aborted", native: windowsWSAConnAborted, sentinel: syscall.ECONNABORTED, category: "connection_aborted", class: "ECONNABORTED"},
		{name: "timeout", native: windowsWSATimedOut, sentinel: syscall.ETIMEDOUT, category: "connection_timeout", class: "ETIMEDOUT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connectError := testConnectError(t, &net.OpError{Err: &os.SyscallError{Err: tt.native}})
			if errors.Is(connectError, tt.sentinel) {
				t.Fatalf("native Winsock errno unexpectedly matched portable sentinel %T", tt.sentinel)
			}
			var syscallError *os.SyscallError
			if !errors.As(connectError, &syscallError) {
				t.Fatal("errors.As did not traverse to os.SyscallError")
			}
			observation := classifyRepositoryError("List.read_tx", connectError)
			if observation.category != tt.category || observation.errnoClass != tt.class {
				t.Fatalf("got category=%q errno_class=%q, want category=%q errno_class=%q", observation.category, observation.errnoClass, tt.category, tt.class)
			}
			if observation.netCauseType != "*os.SyscallError" {
				t.Fatalf("got net_cause_type=%q, want *os.SyscallError", observation.netCauseType)
			}
		})
	}
}

func TestClassifyConnectErrorDNS(t *testing.T) {
	connectError := testLookupConnectError(t, &net.DNSError{Err: "sensitive dns detail", Name: "sensitive.example"})
	observation := classifyRepositoryError("List.read_tx", connectError)
	if observation.category != "dns_error" {
		t.Fatalf("got category=%q, want dns_error", observation.category)
	}
	if observation.connectCauseType != "*net.DNSError" || observation.netCauseType != "none" {
		t.Fatalf("got connect_cause_type=%q net_cause_type=%q", observation.connectCauseType, observation.netCauseType)
	}
}

func TestObserveRepositoryErrorDoesNotLogErrorMessageOrSensitiveValues(t *testing.T) {
	secret := "postgres://user:password@host/db Authorization: Bearer token-value request-body"
	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	err := errors.New(secret)
	if returned := observeRepositoryError("List.page", err); returned != err {
		t.Fatal("repository observation changed the returned error")
	}
	line := output.String()
	for _, prohibited := range []string{"postgres://", "password", "Bearer", "token-value", "request-body"} {
		if strings.Contains(line, prohibited) {
			t.Fatalf("log leaked %q: %s", prohibited, line)
		}
	}
	if !strings.Contains(line, "operation=List.page") || !strings.Contains(line, "category=other") {
		t.Fatalf("missing safe observability fields: %s", line)
	}
}

func TestObserveRepositoryErrorSkipsSuccessfulPath(t *testing.T) {
	var output bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previousWriter) })

	if returned := observeRepositoryError("List.page", nil); returned != nil {
		t.Fatalf("got %v, want nil", returned)
	}
	if output.Len() != 0 {
		t.Fatalf("successful path logged unexpectedly: %s", output.String())
	}
}

func testConnectError(t *testing.T, dialError error) error {
	t.Helper()
	config, err := pgconn.ParseConfig("host=diagnostic.invalid port=5432 user=test-user password=test-password database=test-database sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	config.LookupFunc = func(context.Context, string) ([]string, error) { return []string{"127.0.0.1"}, nil }
	config.DialFunc = func(context.Context, string, string) (net.Conn, error) { return nil, dialError }
	_, err = pgconn.ConnectConfig(context.Background(), config)
	if err == nil {
		t.Fatal("expected connection error")
	}
	var connectError *pgconn.ConnectError
	if !errors.As(err, &connectError) {
		t.Fatalf("got %T, want *pgconn.ConnectError", err)
	}
	return err
}

func testLookupConnectError(t *testing.T, lookupError error) error {
	t.Helper()
	config, err := pgconn.ParseConfig("host=diagnostic.invalid port=5432 user=test-user password=test-password database=test-database sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	config.LookupFunc = func(context.Context, string) ([]string, error) { return nil, lookupError }
	_, err = pgconn.ConnectConfig(context.Background(), config)
	if err == nil {
		t.Fatal("expected connection error")
	}
	var connectError *pgconn.ConnectError
	if !errors.As(err, &connectError) {
		t.Fatalf("got %T, want *pgconn.ConnectError", err)
	}
	return err
}
