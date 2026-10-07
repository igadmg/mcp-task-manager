package task

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidateNameSegment holds the rules every caller-supplied path segment of
// a task's on-disk layout obeys, whatever it names: non-empty, no path
// separator, and not a name made only of dots and spaces (".", "..", ". "
// resolve to a directory, or on Windows to nothing at all). label words the
// message - "filename", "task id", "user".
//
// A leading or trailing space is deliberately left alone: " x " is a legal,
// if odd, file name and has always been accepted.
func ValidateNameSegment(label, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s cannot be empty", label)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%s %q must not contain a path separator", label, name)
	}
	if isDotsAndSpaces(name) {
		return fmt.Errorf("%s %q is not allowed", label, name)
	}
	return nil
}

// ValidateAttachedName reports whether name is usable as the name of a file
// attached to a task. It is the same rule set the storage read and write
// paths apply, exported so a consumer can tell a malformed name from a
// missing file before it tries to read one. It says nothing about the
// reserved names ("{id}.md", "*.phase"): those are a write-only rule, see
// IsReservedFileName.
func ValidateAttachedName(name string) error {
	return ValidateNameSegment("filename", name)
}

// isDotsAndSpaces reports whether name is made of nothing but dots and
// whitespace, the shapes that are not a name at all: "." and ".." are the
// directory itself and its parent, and on Windows a name of dots and spaces
// resolves away to nothing.
func isDotsAndSpaces(name string) bool {
	return strings.TrimFunc(name, func(r rune) bool {
		return r == '.' || unicode.IsSpace(r)
	}) == ""
}
