package response

import (
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Responses struct {
	Message string      `json:"Message" example:"成功"`
	Status  int64       `json:"Status" example:"200"`
	Data    interface{} `json:"Data"`
}

// ErrorResponse for swagger
type ErrorResponse struct {
	Status  int64  `json:"Status" example:"400"` // 狀態碼
	Message string `json:"Message" example:"錯誤"` // 錯誤訊息
}

type Response struct {
	gCtx       *gin.Context
	Message    string
	status     string
	statusCode int64
	errorName  string
	data       interface{}
}

type ErrorInterface interface {
	GetErrorName() string
	GetStatusCode() int64
	GetMessage() string
}

const (
	StatusSuccess  = "Success"
	StatusFail     = "Fail"
	StatusPanic    = "Panic"
	StatusError    = "Error"
	StatusConflict = "Conflict"
)

// New ...
func New(c *gin.Context) *Response {
	return &Response{
		gCtx: c,
	}
}

// Success ..
func (rsp *Response) Success(message string) *Response {
	rsp.statusCode = http.StatusOK
	rsp.status = StatusSuccess
	rsp.Message = message

	return rsp
}

// Fail ...
func (rsp *Response) Fail(code int64, message string) *Response {
	//log.Error("Fail Error, %s", message)
	rsp.statusCode = code
	rsp.status = StatusFail
	rsp.Message = strings.Trim(message, "\b")

	return rsp
}

// Error ...
func (rsp *Response) Error(err interface{}) *Response {
	rsp.status = StatusError
	rsp.setErrorData(err)

	return rsp
}

// Panic ...
func (rsp *Response) Panic(err interface{}) *Response {
	rsp.status = StatusPanic
	rsp.setErrorData(err)

	return rsp
}

// Conflict ...
func (rsp *Response) Conflict(message string) *Response {
	rsp.status = StatusConflict
	rsp.statusCode = 409
	rsp.Message = message

	return rsp
}

func (rsp *Response) setErrorData(err interface{}) {
	rsp.Message = err.(error).Error()

	switch err.(type) {
	case ErrorInterface:
		error := err.(ErrorInterface)
		rsp.statusCode = error.GetStatusCode()
		rsp.errorName = error.GetErrorName()
	default:
		// default errors
		rsp.statusCode = 500
	}
}

// SetData ...
func (rsp *Response) SetData(d interface{}) *Response {
	rsp.data = d
	return rsp
}

func (rsp *Response) SendString() {

	sCode := http.StatusOK
	if rsp.status == StatusPanic {
		sCode = http.StatusInternalServerError
	}

	if rsp.status == StatusConflict {
		sCode = http.StatusConflict
	}

	resp := gin.H{
		"Status":  rsp.statusCode,
		"Data":    rsp.data,
		"Message": rsp.Message,
	}

	rsp.gCtx.JSON(sCode, resp)
}

// Send ...
func (rsp *Response) Send() {
	sCode := http.StatusOK
	if rsp.status == StatusPanic {
		sCode = http.StatusInternalServerError
	}

	if rsp.status == StatusConflict {
		sCode = http.StatusConflict
	}

	resp := gin.H{}

	if rsp.data != nil {
		resp = gin.H{
			"Status":  rsp.statusCode,
			"Data":    rsp.data,
			"Message": rsp.Message,
		}
	} else {
		resp = gin.H{
			"Status":  rsp.statusCode,
			"Message": rsp.Message,
		}
	}

	if sCode != http.StatusOK {
		//log.Debug("Response Data %s %s %s", resp, rsp.gCtx.Request.RequestURI, rsp.gCtx.ClientIP())
		rsp.gCtx.AbortWithStatusJSON(int(rsp.statusCode), resp)
	} else {
		// log.Debug("Response Data %v %s %s", resp, rsp.gCtx.Request.RequestURI, rsp.gCtx.ClientIP())
		rsp.gCtx.JSON(int(rsp.statusCode), resp)
	}
}

// XML 輸出
func (rsp *Response) XML(v interface{}) {
	body, _ := xml.Marshal(v)
	xml := xml.Header + string(body)
	rsp.gCtx.Data(http.StatusOK, `application/xml`, []byte(xml))
}

func (rsp *Response) HTML(name string, data interface{}) {
	rsp.gCtx.HTML(http.StatusOK, name, data)
}
