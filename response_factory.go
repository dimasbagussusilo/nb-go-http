package noob

import (
	"fmt"
	"runtime"
)

func NewResponseSuccess(body ResponseBody, header ...ResponseHeader) Response {
	return NewResponse(StatusOK, body, header...)
}

func NewResponseError(code HTTPStatusCode, body ResponseBody, header ...ResponseHeader) ResponseError {
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		fmt.Println("Failed to get caller information.")
	}

	fmt.Println(fmt.Sprintf("{\"file\": \"%s\",\"line\": %d}", file, line))

	//body.Errors = fmt.Sprintf("{\"file\": \"%s\",\"line\": %d}", file, line)

	h := ResponseHeader{}
	if len(header) > 0 {
		h = header[0]
	}

	return &responseError{
		Response: &response{
			Code:   &code,
			Body:   &body,
			Header: &h,
		},
	}
}
