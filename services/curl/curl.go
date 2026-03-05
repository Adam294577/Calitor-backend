package curl

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"project/services/log"
)

const (
	ContentTypeText   = "text/plain"                        //純文字
	ContentTypeJson   = "application/json"                  //JSON 格式資料
	ContentTypeXml    = "application/xml"                   //XML數據格式
	ContentTypeForm   = "application/x-www-form-urlencoded" //表單默認的提交數據的格式
	ContentTypeBinary = "application/octet-stream"          //未知或二進位資料，通常用於檔案下載
	ContentTypeHtml   = "text/html"                         //HTML文件
)

type Manager struct {
	Path        string
	ContentType string
	Method      string
	Headers     []map[string]string
	Data        string
}

func New() *Manager {
	return &Manager{}
}

func (s *Manager) Get() *Manager {
	s.Method = http.MethodGet
	return s
}

func (s *Manager) Post() *Manager {
	s.Method = http.MethodPost
	return s
}

func (s *Manager) Put() *Manager {
	s.Method = http.MethodPut
	return s
}

func (s *Manager) Delete() *Manager {
	s.Method = http.MethodDelete
	return s
}

func (s *Manager) Patch() *Manager {
	s.Method = http.MethodPatch
	return s
}

func (s *Manager) Authorization(token string) *Manager {
	s.Headers = append(s.Headers, map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", token),
	})
	return s
}

func (s *Manager) SetHeader(key, value string) *Manager {
	s.Headers = append(s.Headers, map[string]string{
		key: value,
	})
	return s
}

func (s *Manager) SetPath(path string) *Manager {
	s.Path = path
	return s
}

func (s *Manager) SetContentType(contentType string) *Manager {
	s.ContentType = contentType
	return s
}

func (s *Manager) SetData(data string) *Manager {
	s.Data = data
	return s
}

func (s *Manager) Send() ([]byte, error) {
	var body io.Reader
	if len(s.Data) != 0 {
		body = bytes.NewBuffer([]byte(s.Data))
	} else {
		body = nil
	}
	request, err := http.NewRequest(s.Method, s.Path, body)
	if err != nil {
		return nil, err
	}
	if len(s.ContentType) != 0 {
		request.Header.Add("Content-Type", s.ContentType)
	} else {
		request.Header.Add("Content-Type", "application/json")
	}
	if len(s.Headers) != 0 {
		for _, header := range s.Headers {
			for key, value := range header {
				request.Header.Add(key, value)
			}
		}
	}
	client := &http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		log.Error("post error: %v", err)
		return nil, err
	}
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 {
		log.Debug("回應內容: %s", string(respBody))
		return respBody, nil
	}
	log.Debug("回應內容: %s", string(respBody))
	return nil, fmt.Errorf("交付失敗")
}
