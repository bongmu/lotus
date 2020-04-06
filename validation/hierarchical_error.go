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
// To achieve this, instead of using `xerrors.wrapError` and keep a reference of
// the child, I define a new struct that references the parent. To keep `xerrors.Is()`
// semantics valid `Unwrap()` now returns the parent error navigating the chain upwards.
// FIXME: This is general enough to be outside of this package.
type HierarchicalError struct {
	parent error
	msg    string
	frame  xerrors.Frame
}

func (e *HierarchicalError) Error() string {
	return fmt.Sprint(e)
}

func (e *HierarchicalError) Format(s fmt.State, v rune) { xerrors.FormatError(e, s, v) }

func (e *HierarchicalError) FormatError(p xerrors.Printer) (next error) {
	p.Print(e.msg)
	e.frame.Format(p)
	return e.parent
	// FIXME: Again, going in the wrong direction.
}

func (e *HierarchicalError) Unwrap() error {
	return e.parent
}

// FIXME: Need to decouple the error itself from its type. Once that is done
//  we can create duplicate errors with different frame info where the unique
//  part is the type the error points to within its hierarchy.
// FIXME: See where we should use this, or if anywhere a `HierarchicalError` is
//  returned (in which case there should be a more automatic way to do it than
//  adding this call in every returned error).
func (e *HierarchicalError) WithFrameInfo() error {
	return &HierarchicalError{parent: e.parent, msg: e.msg, frame: xerrors.Caller(1)}
}

func GetCallerFrame() xerrors.Frame {
	frame := xerrors.Frame{}
	// if internal.EnableTrace {
	frame = xerrors.Caller(1)
	// }
	// FIXME: Can't reference internal package.

	return frame
}

func ErrorWrapString(parent error, msg string) *HierarchicalError {
	return &HierarchicalError{parent: parent, msg: msg, frame: xerrors.Frame{}}
}

func ErrorWrapError(parent error, err error) *HierarchicalError {
	return &HierarchicalError{parent: parent, msg: err.Error(), frame: xerrors.Frame{}}
}
