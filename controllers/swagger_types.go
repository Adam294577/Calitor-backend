package controllers

import (
	response "project/services/responses"
)

// 为 Swagger 定义类型别名
type (
	// Responses 标准响应结构
	Responses = response.Responses
	// ErrorResponse 错误响应结构
	ErrorResponse = response.ErrorResponse
)
