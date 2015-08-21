package posix

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// assert fails the test if the condition is false.
func assert(tb testing.TB, condition bool, msg string, v ...interface{}) {
	if !condition {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("\033[31m%s:%d: "+msg+"\033[39m\n\n", append([]interface{}{filepath.Base(file), line}, v...)...)
		tb.FailNow()
	}
}

// ok fails the test if an err is not nil.
func ok(tb testing.TB, err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("\033[31m%s:%d: unexpected error: %s\033[39m\n\n", filepath.Base(file), line, err.Error())
		tb.FailNow()
	}
}

// equals fails the test if exp is not equal to act.
func equals(tb testing.TB, exp, act interface{}) {
	if !reflect.DeepEqual(exp, act) {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("\033[31m%s:%d:\n\n\texp: %#v\n\n\tgot: %#v\033[39m\n\n", filepath.Base(file), line, exp, act)
		tb.FailNow()
	}
}

var paramtests = []struct {
	in  string
	out string
	err string
}{
	// Basic
	{"${set}", "yes", ""},
	{"${null}", "", ""},
	{"${unset}", "", ""},

	// Names, no brackets
	{"$set", "yes", ""},
	{"$set$set2", "yesyes-two", ""},

	// Default
	{"${set:-word}", "yes", ""},
	{"${null:-word}", "word", ""},
	{"${unset:-word}", "word", ""},

	{"${set-word}", "yes", ""},
	{"${null-word}", "", ""},
	{"${unset-word}", "word", ""},

	// Errors
	{"${set:?word}", "yes", ""},
	{"${null:?word}", "", "word"},
	{"${unset:?word}", "", "word"},

	{"${set?word}", "yes", ""},
	{"${null?word}", "", ""},
	{"${unset?word}", "", "word"},

	{"${set:?}", "yes", ""},
	{"${null:?}", "", "null: parameter null or not set"},
	{"${unset:?}", "", "unset: parameter null or not set"},

	{"${set?}", "yes", ""},
	{"${null?}", "", ""},
	{"${unset?}", "", "unset: parameter null or not set"},

	// Alternative value
	{"${set:+word}", "word", ""},
	{"${null:+word}", "", ""},
	{"${unset:+word}", "", ""},

	{"${set+word}", "word", ""},
	{"${null+word}", "word", ""},
	{"${unset+word}", "", ""},

	// Assignment
	{"${set:=word}", "yes", ""},

	{"${set=word}", "yes", ""},
	{"${null=word}", "", ""},

	// Recursive
	{"foo}bar", "foo}bar", ""},

	{"${null:-${set2}}", "yes-two", ""},
	{"${unset:-${set2}}", "yes-two", ""},

	{"a ${set:-b ${set2} c} d", "a yes d", ""},
	{"a ${null:-b ${set2} c} d", "a b yes-two c d", ""},

	{"${unset-${set2}}", "yes-two", ""},

	{"${null:?${set2}}", "", "yes-two"},
	{"${unset:?${set2}}", "", "yes-two"},

	{"${unset?${set2}}", "", "yes-two"},

	{"${set:+${set2}}", "yes-two", ""},

	{"${set+${set2}}", "yes-two", ""},
	{"${null+${set2}}", "yes-two", ""},

	// Length
	{"${#set}", "3", ""},

	// Quoting
	// backslash outside expansion only applies to $
	{`\"`, `\"`, ""},
	{`\$foo`, `$foo`, ""},

	// quotes outside expansion are unchanged
	{`"foo"`, `"foo"`, ""},
	{`"foo`, `"foo`, ""},
	{`'foo'`, `'foo'`, ""},
	{`'foo`, `'foo`, ""},
	{`"$set"`, `"yes"`, ""},

	// backslash or quotes inside expansion are applied
	{"${unset-'${foo}'}", "${foo}", ""},
	{`${unset-\$foo}`, "$foo", ""},
	{`${unset-\'}`, "'", ""},
	{`${unset-\f}`, `f`, ""},

	// in double-quotes, backslash escape applies to: $ " \ `
	{`${unset-"\$"}`, `$`, ""},
	{`${unset-"\""}`, `"`, ""},
	{`${unset-"\\"}`, `\`, ""},
	{"${unset-\"\\`\"}", "`", ""},

	// in double-quotes, backslash escape does not apply to other characters:
	{`${unset-"\a\b\c"}`, `\a\b\c`, ""},

	// parameters are evaluated inside double-quotes
	{`${unset-a "b ${set} c" d}`, "a b yes c d", ""},

	// Bad syntax
	{"${", "", "unexpected EOF while looking for matching `}'"},
	{"${foo", "", "unexpected EOF while looking for matching `}'"},
	{"${foo-", "", "unexpected EOF while looking for matching `}'"},
	{"${#foo", "", "unexpected EOF while looking for matching `}'"},
	{"${unset-'foo", "", "unexpected EOF while looking for matching `''"},

	{"foo$", "foo$", ""},
}

func TestExpand_simple(t *testing.T) {
	mapping := map[string]string{
		"set":  "yes",
		"set2": "yes-two",
		"null": "",
	}

	for _, tt := range paramtests {
		x, err := Expand(tt.in, Map(mapping))
		if tt.err != "" {
			if err == nil || err.Error() != tt.err {
				t.Errorf("pattern %#v should have produced error %#v, but got: %s", tt.in, tt.err, err)
			}
		} else if err != nil {
			t.Errorf("pattern %#v should not have produced an error, but got: %s", tt.in, err)
		}
		if x != tt.out {
			t.Errorf("pattern %#v should expand to %#v, but got %#v", tt.in, tt.out, x)
		}
	}
}

func TestExand_assignReadOnlyFunc(t *testing.T) {
	_, err := Expand("${unset:=word}", Func(func(s string) string {
		return ""
	}))
	if err == nil {
		t.Fatal("assignment on read-only function should return an error")
	}
}

func TestExand_assignReadOnlyMap(t *testing.T) {
	_, err := Expand("${unset:=word}", Map(nil))
	if err == nil {
		t.Fatal("assignment on read-only map should return an error")
	}
}

func TestExpand_assign(t *testing.T) {
	mapping := map[string]string{"null": ""}
	x, err := Expand("${null:=word}", RWMap(mapping))
	ok(t, err)
	equals(t, "word", x)
	equals(t, map[string]string{"null": "word"}, mapping)

	mapping = map[string]string{}
	x, err = Expand("${unset:=word}", RWMap(mapping))
	ok(t, err)
	equals(t, "word", x)
	equals(t, map[string]string{"unset": "word"}, mapping)

	mapping = map[string]string{}
	x, err = Expand("${unset=word}", RWMap(mapping))
	ok(t, err)
	equals(t, "word", x)
	equals(t, map[string]string{"unset": "word"}, mapping)
}
