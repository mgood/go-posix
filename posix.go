package posix

import (
	"os"
	"syscall"
)

// Getter is the interface for mapping key to value lookups.
//
type Getter interface {
	Get(key string) (value string, exists bool)
}

// Setter is the interface for mutable mappings to update a key.
type Setter interface {
	Set(key string, value string) error
}

// Func implements the Getter interface for simple lookup functions.
type Func func(string) string

func (f Func) Get(s string) (string, bool) {
	return f(s), true
}

// Map implements the Getter interface for map[string]string
type Map map[string]string

func (m Map) Get(k string) (string, bool) {
	v, ok := m[k]
	return v, ok
}

// RWMap implements the Getter and Setter interfaces for map[string]string.
type RWMap map[string]string

func (m RWMap) Get(k string) (string, bool) {
	return Map(m).Get(k)
}

func (m RWMap) Set(k, v string) error {
	m[k] = v
	return nil
}

// Expand replaces ${var} or $var in the string based on the mapping.
// Supports most Posix shell exapansions:
//
// Default: ${param:-word} ${param-word}
//
// Assign default: ${param:=word} ${param=word}
//
// Error: ${param:?error} ${param?error}
//
// Alternative: ${param:+word} ${param+word}
//
// See: http://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html
func Expand(s string, mapping Getter) (string, error) {
	lexer := lex(s)
	val, err := evalStream(mapping, lexer.stream)
	lexer.Close()
	return val, err
}

// ExpandEnv replaces ${var} or $var in the string according to the values of
// the current environment variables.
func ExpandEnv(s string) (string, error) {
	return Expand(s, osEnviron)
}

type environGetSetter struct{}

func (e environGetSetter) Get(k string) (string, bool) {
	return syscall.Getenv(k)
}

func (e environGetSetter) Set(k, v string) error {
	return os.Setenv(k, v)
}

var osEnviron environGetSetter = environGetSetter{}
