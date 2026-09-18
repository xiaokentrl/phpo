// 带错误码的包装错误：UI 按 Code 呈现、日志按 Code 归因
package errs

import "fmt"

type Error struct {
	Code string
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

func New(code, msg string) *Error {
	return &Error{Code: code, Msg: msg}
}

func Wrap(code, msg string, err error) *Error {
	return &Error{Code: code, Msg: msg, Err: err}
}
