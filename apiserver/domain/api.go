package domain

const DefaultPaginationSize = 20

type ApiCode uint8

const (
	ApiCodeSuccess ApiCode = iota
	ApiCodeInput   ApiCode = 1
	ApiCodeSystem  ApiCode = 99
)

type ApiResponse[T any] struct {
	Code    ApiCode `json:"code"`
	Message string  `json:"message,omitzero"`
	Data    T       `json:"data,omitzero"`
}

type EmptyPayload struct{}

func (c ApiCode) WithError(err error) ApiResponse[EmptyPayload] {
	return ApiResponse[EmptyPayload]{
		Code:    c,
		Message: err.Error(),
	}
}

func (c ApiCode) WithData[T any](data T) ApiResponse[T] {
	return ApiResponse[T]{
		Code: c,
		Data: data,
	}
}
