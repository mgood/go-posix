package posix

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"unicode/utf8"
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

// Expand replaces ${var} or $var in the string based on the mapping.
func Expand(s string, mapping Getter) (string, error) {
	return lex(s, mapping)
}

func ExpandEnv(s string) (string, error) {
	return Expand(s, osEnviron)
}

type stateFn func(*lexer) stateFn

type Pos int

type lexer struct {
	mapping Getter
	items   chan item
	input   string
	state   stateFn
	pos     Pos
	start   Pos
	width   Pos
	depth   int
}

type item interface {
	Eval(mapping Getter, stream chan item) (string, error)
}

type itemEndBracket struct{}

func (x itemEndBracket) Eval(mapping Getter, stream chan item) (string, error) {
	return "", nil
}

var endBracket itemEndBracket

type itemText string

func (p itemText) Eval(mapping Getter, stream chan item) (string, error) {
	return string(p), nil
}

type itemReadParam string

func (p itemReadParam) Eval(mapping Getter, stream chan item) (string, error) {
	v, _ := mapping.Get(string(p))
	return v, nil
}

type itemParamLen string

func (p itemParamLen) Eval(mapping Getter, stream chan item) (string, error) {
	v, _ := mapping.Get(string(p))
	return strconv.Itoa(len(v)), nil
}

type itemParamOp struct {
	parameter   string
	op          rune
	nullIsEmpty bool
}

func (p itemParamOp) Eval(mapping Getter, stream chan item) (string, error) {
	paramVal, paramSet := mapping.Get(p.parameter)
	if p.nullIsEmpty {
		paramSet = paramVal != ""
	}

	if p.op == '+' {
		if paramSet {
			return evalStream(mapping, bracketedStream(stream))
		}
		skipStream(stream)
		return "", nil
	}

	if paramSet {
		skipStream(bracketedStream(stream))
		return paramVal, nil
	}

	val, err := evalStream(mapping, bracketedStream(stream))
	if err != nil {
		return "", err
	}

	switch p.op {
	case '-':
		return val, nil
	case '=':
		if setter, ok := mapping.(Setter); ok {
			err := setter.Set(p.parameter, val)
			if err != nil {
				return "", err
			}
			return val, nil
		}
		// XXX
		return "", errors.New("setting not supported")
	case '?':
		if val == "" {
			val = fmt.Sprintf("%s: parameter null or not set", p.parameter)
		}
		return "", errors.New(val)
	}

	return "", fmt.Errorf("unexpected op: %s", p.op)
}

func evalStream(mapping Getter, stream chan item) (string, error) {
	var buf bytes.Buffer

	for item := range stream {
		text, err := item.Eval(mapping, stream)
		if err != nil {
			return "", err
		}
		buf.WriteString(text)
	}

	return buf.String(), nil
}

type itemUnexpectedEOF rune

func (i itemUnexpectedEOF) Eval(mapping Getter, stream chan item) (string, error) {
	return "", fmt.Errorf("unexpected EOF while looking for matching `%c'", i)
}

func bracketedStream(stream chan item) chan item {
	c := make(chan item)
	go func() {
		for item := range stream {
			if _, ok := item.(itemEndBracket); ok {
				close(c)
				return
			}
			c <- item
		}
		c <- itemUnexpectedEOF('}')
	}()
	return c
}

func skipStream(stream chan item) {
	for _ = range stream {
	}
}

func lex(s string, mapping Getter) (string, error) {
	l := &lexer{
		mapping: mapping,
		items:   make(chan item),
		input:   s,
	}
	go l.run()
	val, err := evalStream(mapping, l.items)
	skipStream(l.items)
	return val, err
}

const eof = -1

// next returns the next rune in the input.
func (l *lexer) next() rune {
	if int(l.pos) >= len(l.input) {
		l.width = 0
		return eof
	}
	r, w := utf8.DecodeRuneInString(l.input[l.pos:])
	l.width = Pos(w)
	l.pos += l.width
	return r
}

// backup steps back one rune. Can only be called once per call of next.
func (l *lexer) backup() {
	l.pos -= l.width
}

func (l *lexer) emitLastToken() {
	l.backup()
	if l.pos > l.start {
		l.emit(itemText(l.token()))
		l.start = l.pos
	}
	l.next()
	l.ignore()
}

func (l *lexer) token() string {
	return l.input[l.start:l.pos]
}

func (l *lexer) emit(item item) {
	l.items <- item
}

// ignore skips over the pending input before this point.
func (l *lexer) ignore() {
	l.start = l.pos
}

func (l *lexer) run() {
	for l.state = lexText; l.state != nil; {
		l.state = l.state(l)
	}
	close(l.items)
}

func lexText(l *lexer) stateFn {
	for {
		switch l.next() {
		case eof:
			l.emitLastToken()
			return nil
		case '}':
			if l.depth > 0 {
				l.emitLastToken()
				l.emit(endBracket)
				return lexEndBracket
			}
		case '$':
			l.emitLastToken()
			return lexStartExpansion
		case '\'':
			l.emitLastToken()
			return lexSingleQuoteString
		case '\\':
			l.emitLastToken()
			l.next()
		}
	}
}

func lexSingleQuoteString(l *lexer) stateFn {
	for {
		switch l.next() {
		case eof:
			l.emit(itemUnexpectedEOF('\''))
			return nil
		case '\'':
			l.emitLastToken()
			return lexText
		}
	}
}

func lexStartExpansion(l *lexer) stateFn {
	c := l.next()
	switch {
	case c == eof:
		l.emit(itemText("$"))
		return nil
	case c == '{':
		l.ignore()
		l.depth++
		return lexBracketName
	case isAlpha(c):
		return lexSimpleName
	}
	return nil // FIXME
}

func lexEndBracket(l *lexer) stateFn {
	l.depth--
	l.ignore()
	return lexText
}

func lexSimpleName(l *lexer) stateFn {
	for {
		if !isAlphaNum(l.next()) {
			l.backup()
			name := l.token()
			l.emit(itemReadParam(name))
			l.ignore()
			return lexText
		}
	}
}

func lexBracketName(l *lexer) stateFn {
	if l.next() == '#' {
		l.ignore()
		return lexParamLength
	}
	l.backup()
	for {
		switch l.next() {
		case eof:
			l.emit(itemUnexpectedEOF('}'))
			return nil
		// FIXME what if ':' is not followed by an op?
		case '}', ':', '-', '?', '+', '=':
			l.backup()
			return lexParamOp
		}
	}
}

func lexParamOp(l *lexer) stateFn {
	paramName := l.token()

	op := l.next()
	if op == '}' {
		l.emit(itemReadParam(paramName))
		return lexEndBracket
	}

	nullIsEmpty := op == ':'
	if nullIsEmpty {
		op = l.next()
	}
	l.ignore()

	l.emit(itemParamOp{paramName, op, nullIsEmpty})
	return lexText
}

func lexParamLength(l *lexer) stateFn {
	for {
		switch l.next() {
		case eof:
			l.emit(itemUnexpectedEOF('}'))
			return nil
		case '}':
			l.backup()
			name := l.token()
			l.emit(itemParamLen(name))
			l.next()
			l.ignore()
			return lexEndBracket
		}
	}
}

// isAlpha reports whether the byte is an ASCII letter or underscore
func isAlpha(c rune) bool {
	return c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// isNum reports whether the byte is an ASCII number
func isNum(c rune) bool {
	return '0' <= c && c <= '9'
}

// isAlphaNum reports whether the byte is an ASCII letter, number, or underscore
func isAlphaNum(c rune) bool {
	return isAlpha(c) || isNum(c)
}
