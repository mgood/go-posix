package posix

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

type Getter interface {
	Get(string) (string, bool)
}

type Setter interface {
	Set(string, string) error
}

type Func func(string) string

func (f Func) Get(s string) (string, bool) {
	return f(s), true
}

type Map map[string]string

func (m Map) Get(k string) (string, bool) {
	v, ok := m[k]
	return v, ok
}

type RWMap map[string]string

func (m RWMap) Get(k string) (string, bool) {
	return Map(m).Get(k)
}

func (m RWMap) Set(k, v string) error {
	m[k] = v
	return nil
}

type environGetSetter struct{}

func (e environGetSetter) Get(k string) (string, bool) {
	return syscall.Getenv(k)
}

func (e environGetSetter) Set(k, v string) error {
	return os.Setenv(k, v)
}

var osEnviron environGetSetter = environGetSetter{}

// Expand replaces ${var} or $var in the string based on the mapping function.
// For example, os.ExpandEnv(s) is equivalent to os.Expand(s, os.Getenv).
func Expand(s string, mapping Getter) (string, error) {
	buf := make([]byte, 0, 2*len(s))
	// ${} is all ASCII, so bytes are fine for this operation.
	i := 0
	for j := 0; j < len(s); j++ {
		if s[j] == '$' && j+1 < len(s) {
			buf = append(buf, s[i:j]...)
			name, w := getShellName(s[j+1:])
			sub, err := subShellName(mapping, name)
			if err != nil {
				return "", err
			}
			buf = append(buf, sub...)
			j += w
			i = j + 1
		}
	}
	return string(buf) + s[i:], nil
}

func ExpandEnv(s string) (string, error) {
	return Expand(s, osEnviron)
}

// getName returns the name that begins the string and the number of bytes
// consumed to extract it.  If the name is enclosed in {}, it's part of a ${}
// expansion and two more bytes are needed than the length of the name.
func getShellName(s string) (string, int) {
	switch {
	case s[0] == '{':
		if len(s) > 2 && isShellSpecialVar(s[1]) && s[2] == '}' {
			return s[1:2], 3
		}
		// Scan to closing brace
		depth := 1
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 {
				return s[1:i], i + 1
			}
		}
		return "", 1 // Bad syntax; just eat the brace.
	case isShellSpecialVar(s[0]):
		return s[0:1], 1
	}
	// Scan alphanumerics.
	var i int
	for i = 0; i < len(s) && isAlphaNum(s[i]); i++ {
	}
	return s[:i], i
}

func subShellName(mapping Getter, s string) (string, error) {
	parameter, op, word := splitShellName(s)
	paramVal, paramSet := mapping.Get(parameter)
	if op == "" {
		return paramVal, nil
	} else if op == "len" {
		return strconv.Itoa(len(paramVal)), nil
	}
	fallback := !paramSet || (op[0] == ':' && paramVal == "")
	opCode := op[len(op)-1]

	if opCode == '+' {
		if fallback {
			return "", nil
		} else {
			return Expand(word, mapping)
		}
	}

	if !fallback {
		return paramVal, nil
	}

	if opCode == '?' && word == "" {
		return "", fmt.Errorf("%s: parameter null or not set", parameter)
	}

	word, err := Expand(word, mapping)
	if err != nil {
		return "", err
	}

	switch opCode {
	case '-':
		return word, nil
	case '?':
		return "", errors.New(word)
	case '=':
		if setter, ok := mapping.(Setter); ok {
			err := setter.Set(parameter, word)
			if err != nil {
				return "", err
			}
			return word, nil
		} else {
			return "", fmt.Errorf("mapping type %T does not support assignment for %#v", mapping, parameter)
		}
	}

	return "", fmt.Errorf("unexpected op %s", opCode)
}

func splitShellName(s string) (string, string, string) {
	// TODO len(s) == 0?
	if s[0] == '#' {
		return s[1:], "len", ""
	}

	// FIXME what if string starts with an op, e.g. param name is empty?
	for i := 1; i < len(s); i++ {
		if isSubOp(s[i]) {
			j := 0
			if s[i-1] == ':' {
				j = 1
			}
			return s[:i-j], s[i-j : i+1], s[i+1:]
		}
	}
	return s, "", ""
}

func isSubOp(c uint8) bool {
	switch c {
	case '-', '?', '+', '=':
		return true
	}
	return false
}

// isSpellSpecialVar reports whether the character identifies a special
// shell variable such as $*.
func isShellSpecialVar(c uint8) bool {
	switch c {
	case '*', '#', '$', '@', '!', '?', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

// isAlphaNum reports whether the byte is an ASCII letter, number, or underscore
func isAlphaNum(c uint8) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}
