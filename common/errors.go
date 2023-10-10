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

func NewErrorWCode(code int, errStr string) ErrorWCode {
	return &errorWCode{
		error: errors.New(errStr),
		code:  code,
	}
}

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

type T interface {
	Fatalf(format string, args ...interface{})
	TempDir() string
}

// DealWithErr panics if err is not nil.
func DealWithErr(v interface{}) {
	defer RecoverStack()
	if v != nil {
		panic(v)
	}
}

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

func FailIfErr(t T, err error) {
	if err != nil {
		t.Fatalf("'%v'\n%v", err, GetStack())
	}
}
func ExpectError(t T, current error, expected error) {
	if current != expected {
		expectError(t, fmt.Sprintf("%v", current), fmt.Sprintf("%v", expected), GetStack())
	}
}
func ExpectBytes(t T, current []byte, expected string) {
	ExpectString(t, hexutil.Encode(current), expected)
}
func ExpectTrue(t T, value bool) {
	if !value {
		expectError(t, "False", "True", GetStack())
	}
}
func ExpectUint64(t T, current, expected uint64) {
	if current != expected {
		expectError(t, fmt.Sprintf("%v", current), fmt.Sprintf("%v", expected), GetStack())
	}
}
func ExpectAmount(t T, current, expected *big.Int) {
	if current.Cmp(expected) != 0 {
		expectError(t, current.String(), expected.String(), GetStack())
	}
}
func ExpectString(t T, current, expected string) {
	current = trimEol(current)
	expected = trimEol(expected)
	if current != expected {
		expectError(t, current, expected, GetStack())
	}
}
func ExpectJson(t T, current interface{}, expected string) {
	strBytes, err := json.MarshalIndent(current, "", "\t")
	FailIfErr(t, err)
	ExpectString(t, string(strBytes), expected)
}
func Expect(t T, current, expected interface{}) {
	currentStr := trimEol(fmt.Sprintf("%v", current))
	expectedStr := trimEol(fmt.Sprintf("%v", expected))
	if currentStr != expectedStr {
		expectError(t, currentStr, expectedStr, GetStack())
	}
}

type Expecter struct {
	hideHash bool
	subJson  interface{}

	receivedF   func() (string, error)
	received    string
	receivedErr error
	stack       string
}

func String(received string) *Expecter {
	return &Expecter{
		hideHash:    false,
		received:    received,
		receivedErr: nil,
	}
}
func Json(j interface{}, inheritedError error) *Expecter {
	receivedBytes, err := json.MarshalIndent(j, "", "\t")
	DealWithErr(err)
	return &Expecter{
		received:    string(receivedBytes),
		receivedErr: inheritedError,
	}
}
func LateCaller(f func() (string, error)) *Expecter {
	return &Expecter{
		receivedF: f,
		stack:     GetStack(),
	}
}

func HideHashes(a string) string {
	a = regexp.MustCompile(`[0-9a-f]{64}`).ReplaceAllString(a, "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
	a = regexp.MustCompile(`[A-Za-z0-9+/]{86}==`).ReplaceAllString(a, "XXXSIGNATUREXINXBASEX64XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
	return a
}
func (exp *Expecter) HideHashes() *Expecter {
	exp.hideHash = true
	return exp
}
func (exp *Expecter) SubJson(subJson interface{}) *Expecter {
	exp.subJson = subJson
	return exp
}

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
