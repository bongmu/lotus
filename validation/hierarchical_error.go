package validation

import (
	"fmt"

	"golang.org/x/xerrors"
)

// This is incorrect. Its purpose is to illustrate my intent to see if there's a
// clean and simple way to do this (and even if it's worth doing it). `Errorf`,
//  * https://pkg.go.dev/golang.org/x/xerrors?tab=doc#Errorf
// allows to wrap a child error into a new parent error (using OOP language here
// is already wrong). In this way one error can have many parents, what I would
// like is a hierarchy of errors where one parent has many possible child errors
// according to its category. The objective is to programmatically  preserve in
// the language information liking errors from different contexts.
// FIXME: This is general enough to be outside of this package.
type HierarchicalErrorClass struct {
	parent      *HierarchicalErrorClass
	description string
}

func NewHierarchicalErrorClass(description string) *HierarchicalErrorClass {
	return &HierarchicalErrorClass{description: description}
}

func (c *HierarchicalErrorClass) Child(description string) *HierarchicalErrorClass {
	return &HierarchicalErrorClass{parent: c, description: description}
}

func (c *HierarchicalErrorClass) NewError() *ErrorWithClass {
	return &ErrorWithClass{class: c, msg: c.description, frame: GetCallerFrame()}
}

func (c *HierarchicalErrorClass) FromString(msg string) *ErrorWithClass {
	return &ErrorWithClass{class: c, msg: msg, frame: GetCallerFrame()}
}

func (c *HierarchicalErrorClass) WrapError(err error) *ErrorWithClass {
	return &ErrorWithClass{wrappedError: err, msg: c.description, frame: GetCallerFrame()}
}

// Copied from `xerrors.wrapError`, includes a `class` attribute information.
type ErrorWithClass struct {
	class *HierarchicalErrorClass
	// FIXME: Should this be a pointer?

	wrappedError error
	msg          string
	frame        xerrors.Frame
	// FIXME: Can be wrapped in the standard `xerrors` way independent of its class?
}

func (e *ErrorWithClass) Class() *HierarchicalErrorClass {
	return e.class
}

// FIXME: Why can't it have a pointer receiver to satisfy the `error` interface?
//  (`xerrors.wrapError` uses pointer)
func (e ErrorWithClass) Error() string {
	return fmt.Sprint(e)
}

func (e *ErrorWithClass) Format(s fmt.State, v rune) { xerrors.FormatError(e, s, v) }

func (e *ErrorWithClass) FormatError(p xerrors.Printer) (next error) {
	p.Print(e.msg)
	// FIXME: Maybe include the `class` description here.
	e.frame.Format(p)
	return e.wrappedError
}

func (e *ErrorWithClass) Unwrap() error {
	return e.wrappedError
}

func GetCallerFrame() xerrors.Frame {
	frame := xerrors.Frame{}
	// if internal.EnableTrace {
	frame = xerrors.Caller(1)
	// }
	// FIXME: Can't reference internal package, if the flag is necessary we can extract
	//  it from an oracle of `xerrors` behavior (hacky).

	return frame
}
