// Package common provides the shared primitives used throughout the
// node: coded errors that satisfy the JSON-RPC error contract together
// with panic/stack helpers, the structured (log15) loggers for each
// subsystem and the file-based log setup, big-endian byte codecs for
// integers and big.Int values, frequently used big.Int constants, a
// process-wide mockable clock, and small concurrency primitives
// (cooperatively cancellable background tasks and tick arithmetic over
// fixed time intervals).
//
// The package also exports a snapshot-style test toolkit (the Expect*
// functions and the Expecter builder) that is shared by test suites
// across the repository; it lives in non-test files so that other
// packages can import it.
package common

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/inconshreveable/log15"
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/rpc/server"
)

// NewErrorWCode returns a sentinel error that carries a numeric error
// code alongside its message. Coded errors are declared as package-level
// variables (for example in rpc/api/errors.go) and travel through the
// RPC layer, where rpc/server serializes the code into the JSON-RPC
// error object via the server.Error interface.
func NewErrorWCode(code int, errStr string) ErrorWCode {
	return &errorWCode{
		error: errors.New(errStr),
		code:  code,
	}
}

// ErrorWCode is an error with a stable numeric code (server.Error) that
// can be specialized with extra context. AddDetail returns a new error
// that keeps the original code while the message becomes
// "base message;detail". The detailed error does not match the base via
// == or errors.Is (the implementation embeds the error interface, which
// promotes no Unwrap method); only the shared code links them. The base
// sentinel is never mutated, which makes coded errors safe to share as
// package-level variables.
type ErrorWCode interface {
	server.Error
	//AddSubErr(err error) ErrorWCode
	AddDetail(detail string) ErrorWCode
}
type errorWCode struct {
	error
	code int
}

func (err *errorWCode) ErrorCode() int {
	return err.code
}
func (err *errorWCode) AddDetail(detail string) ErrorWCode {
	return &errorWCode{
		code:  err.code,
		error: fmt.Errorf("%w;%v", err.error, detail),
	}
}

// T is the minimal subset of *testing.T required by the Expect* helpers
// in this package, so they can also be driven by other harnesses that
// can fail a test and provide a temporary directory.
type T interface {
	Fatalf(format string, args ...interface{})
	TempDir() string
}

// DealWithErr panics if v is not nil. The deferred RecoverStack logs the
// panic together with a stack trace and then re-panics, so the panic
// still propagates to the caller; this is the package's idiom for
// asserting that an error "cannot happen".
func DealWithErr(v interface{}) {
	defer RecoverStack()
	if v != nil {
		panic(v)
	}
}

// RecoverStack is meant to be deferred. If the goroutine is panicking it
// logs the panic value annotated with a stack trace (and prints it to
// stdout) and then panics again with the original value; it never
// swallows the panic. When there is no panic in flight it does nothing.
func RecoverStack() {
	if err := recover(); err != nil {
		var e error
		switch t := err.(type) {
		case error:
			e = errors.WithStack(t)
		case string:
			e = errors.New(t)
		default:
			e = errors.Errorf("unknown type %+v", err)
		}

		log15.Error("panic", "err", err, "withstack", e)
		fmt.Printf("%+v", e)
		panic(err)
	}
}

var (
	includeFiles = []string{
		"_test.go",
	}
	excludeFiles = []string{}
)

func trimEol(a string) string {
	for len(a) != 0 && a[0] == '\n' {
		a = a[1:]
	}
	for len(a) != 0 && a[len(a)-1] == '\n' {
		a = a[0 : len(a)-1]
	}
	return a
}

// expect(hash.toString('hex')).toEqual('1f2547448d68fd2d6e0736300eae49fad255016a8bf9aa95cd52973980abe53');
// Expected: "1f2547448d68fd2d6e0736300eae49fad255016a8bf9aa95cd52973980abe53"
// Received: "1f2547448d68fd2d6e0736300eae49fad255016a8bf9aa95cd52973980abe533"
func expectError(t T, received, expected, stack string) {
	expected = trimEol(expected)
	received = trimEol(received)
	t.Fatalf("\n<<<<<<< Expected\n%v\n=======\n%v\n>>>>>>> Received\n%v\n", expected, received, stack)
}
func expectString(t T, current, expected, stack string) {
	current = trimEol(current)
	expected = trimEol(expected)
	if current != expected {
		expectError(t, current, expected, stack)
	}
}

// GetStack returns the single stack frame of the innermost caller whose
// file name matches the include list (files ending in _test.go), so test
// failures are reported at the line in the test that triggered them. If
// no test frame is found it falls back to the goroutine header line.
func GetStack() string {
	st := string(debug.Stack())
	frames := strings.Split(st, "\n")
	for i := 2; i < len(frames); i += 2 {
		ok := false
		for _, file := range includeFiles {
			if strings.Contains(frames[i], file) {
				ok = true
			}
		}

		if !ok {
			continue
		}

		for _, file := range excludeFiles {
			if strings.Contains(frames[i], file) {
				ok = false
			}
		}

		if ok {
			return frames[i]
		}
	}
	return frames[0]
}

// FailIfErr fails the test immediately if err is not nil, reporting the
// error together with the calling test's stack frame.
func FailIfErr(t T, err error) {
	if err != nil {
		t.Fatalf("'%v'\n%v", err, GetStack())
	}
}

// ExpectError fails the test unless current and expected are the same
// error value. The comparison uses ==, not errors.Is, so a wrapped or
// detail-annotated error does not match its base sentinel.
func ExpectError(t T, current error, expected error) {
	if current != expected {
		expectError(t, fmt.Sprintf("%v", current), fmt.Sprintf("%v", expected), GetStack())
	}
}

// ExpectBytes fails the test unless current encodes to the expected
// 0x-prefixed hexadecimal string.
func ExpectBytes(t T, current []byte, expected string) {
	ExpectString(t, hexutil.Encode(current), expected)
}

// ExpectTrue fails the test if value is false.
func ExpectTrue(t T, value bool) {
	if !value {
		expectError(t, "False", "True", GetStack())
	}
}

// ExpectUint64 fails the test unless current equals expected.
func ExpectUint64(t T, current, expected uint64) {
	if current != expected {
		expectError(t, fmt.Sprintf("%v", current), fmt.Sprintf("%v", expected), GetStack())
	}
}

// ExpectAmount fails the test unless the two big.Int values are
// numerically equal (compared with Cmp, so distinct pointers to the same
// value match).
func ExpectAmount(t T, current, expected *big.Int) {
	if current.Cmp(expected) != 0 {
		expectError(t, current.String(), expected.String(), GetStack())
	}
}

// ExpectString fails the test unless current equals expected after
// stripping leading and trailing newlines from both, which lets tests
// declare expected values as raw multi-line string literals.
func ExpectString(t T, current, expected string) {
	current = trimEol(current)
	expected = trimEol(expected)
	if current != expected {
		expectError(t, current, expected, GetStack())
	}
}

// ExpectJson fails the test unless current, marshaled as tab-indented
// JSON, equals the expected string.
func ExpectJson(t T, current interface{}, expected string) {
	strBytes, err := json.MarshalIndent(current, "", "\t")
	FailIfErr(t, err)
	ExpectString(t, string(strBytes), expected)
}

// Expect fails the test unless current and expected have the same
// default (%v) string representation, after stripping leading and
// trailing newlines from both.
func Expect(t T, current, expected interface{}) {
	currentStr := trimEol(fmt.Sprintf("%v", current))
	expectedStr := trimEol(fmt.Sprintf("%v", expected))
	if currentStr != expectedStr {
		expectError(t, currentStr, expectedStr, GetStack())
	}
}

// Expecter is a snapshot-test builder. It captures a received value
// (eagerly via String or Json, or lazily via LateCaller), optionally
// post-processes it (HideHashes, SubJson), and finally compares it
// against an inline expectation with Equals or asserts the produced
// error with Error.
type Expecter struct {
	hideHash bool
	subJson  interface{}

	receivedF   func() (string, error)
	received    string
	receivedErr error
	stack       string
}

// String starts an expectation on a string value that was already
// produced without error.
func String(received string) *Expecter {
	return &Expecter{
		hideHash:    false,
		received:    received,
		receivedErr: nil,
	}
}

// Json starts an expectation on j rendered as tab-indented JSON.
// inheritedError is the error returned by whatever call produced j;
// Equals fails the test if it is not nil, while Error asserts its value,
// so call sites can pass a (value, error) pair straight through.
func Json(j interface{}, inheritedError error) *Expecter {
	receivedBytes, err := json.MarshalIndent(j, "", "\t")
	DealWithErr(err)
	return &Expecter{
		received:    string(receivedBytes),
		receivedErr: inheritedError,
	}
}

// LateCaller starts an expectation whose received value is produced by f
// only when Equals or Error runs, while the reported stack frame is
// captured here, at the point where the expectation was declared. Used
// for values that keep changing until the end of the test, such as
// accumulated log output (see SaveLogs).
func LateCaller(f func() (string, error)) *Expecter {
	return &Expecter{
		receivedF: f,
		stack:     GetStack(),
	}
}

// HideHashes replaces every 64-character lowercase hexadecimal string
// with a fixed XXXHASHXXX... placeholder and every 86-character base64
// string ending in == with a fixed XXXSIGNATURE... placeholder of the
// same length, so snapshots stay stable when hashes or signatures vary
// between runs.
func HideHashes(a string) string {
	a = regexp.MustCompile(`[0-9a-f]{64}`).ReplaceAllString(a, "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
	a = regexp.MustCompile(`[A-Za-z0-9+/]{86}==`).ReplaceAllString(a, "XXXSIGNATUREXINXBASEX64XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
	return a
}

// HideHashes makes the final comparison run the received value through
// the package-level HideHashes, masking hashes and signatures. It
// returns the same Expecter for chaining.
func (exp *Expecter) HideHashes() *Expecter {
	exp.hideHash = true
	return exp
}

// SubJson narrows the comparison to a projection of the received JSON:
// before comparing, the received text is unmarshaled into subJson and
// re-marshaled, so only the fields of subJson's type survive. It returns
// the same Expecter for chaining.
func (exp *Expecter) SubJson(subJson interface{}) *Expecter {
	exp.subJson = subJson
	return exp
}

// Equals resolves the received value (invoking the LateCaller function
// if one was set), fails the test if an error was produced along the
// way, applies the configured SubJson projection and hash masking, and
// finally fails the test unless the result equals expected after
// stripping leading and trailing newlines from both.
func (exp *Expecter) Equals(t T, expected string) {
	received := exp.received
	if exp.receivedF != nil {
		received, exp.receivedErr = exp.receivedF()
	}
	if exp.receivedErr != nil {
		t.Fatalf("got error '%v' when expecting a clean execution", exp.receivedErr)
	}
	if exp.subJson != nil && received != "null" {
		err := json.Unmarshal([]byte(received), exp.subJson)
		FailIfErr(t, err)
		receivedBytes, err := json.MarshalIndent(exp.subJson, "", "\t")
		FailIfErr(t, err)
		received = string(receivedBytes)
	}
	if exp.hideHash {
		received = HideHashes(received)
	}
	if exp.stack == "" {
		exp.stack = GetStack()
	}
	expectString(t, received, expected, exp.stack)
}

// Error resolves the received value (invoking the LateCaller function if
// one was set) and fails the test unless the error produced along the
// way has the same message as err; a nil error matches only a nil err,
// since both format as "<nil>".
func (exp *Expecter) Error(t T, err error) {
	if exp.receivedF != nil {
		_, exp.receivedErr = exp.receivedF()
	}
	received := fmt.Sprintf("%v", exp.receivedErr)
	if exp.stack == "" {
		exp.stack = GetStack()
	}
	expectString(t, received, fmt.Sprintf("%v", err), exp.stack)
}
