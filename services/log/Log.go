package log

import (
	"fmt"
	"log"
	"os"
	"project/services/common"
	"runtime"
	"strings"
	"time"
)

const (
	LevelEmergency     = iota // 系統等級緊急，比如硬碟出錯，記憶體異常，網路不可用等..
	LevelAlert                // 系統等級警告，比如資料庫訪問異常，配置文件出錯等
	LevelCritical             // 系統等級危險，比如權限出錯，訪問異常等
	LevelError                // 用戶等級錯誤
	LevelWarning              // 用戶等級警告
	LevelInformational        // 用戶等級信息
	LevelDebug                // 用戶等級測試
	LevelTrace                // 用戶等級基本輸出
)

var LevelMap = map[int]string{
	LevelEmergency:     "EMER",
	LevelAlert:         "ALERT",
	LevelCritical:      "CRITICAL",
	LevelError:         "ERROR",
	LevelWarning:       "WARN",
	LevelInformational: "INFO",
	LevelDebug:         "DEBUG",
	LevelTrace:         "TRAC",
}

// LogsWrite 寫入日誌
func LogsWrite(level int, format string, args ...interface{}) {
	// 資料夾路徑
	path := fmt.Sprintf("%s/logs", "storage")
	// 判斷資料夾是石存在
	if exist, _ := common.FilePathExist(path); !exist {
		// 以日期建立資料夾
		if err := os.MkdirAll(path, os.ModePerm); err != nil {
			fmt.Printf("資料夾建立失敗 => %v \n", err)
		}
	}
	// 檔案訊息
	fileName := fmt.Sprintf("%s/logs_%s.log", path, common.GetTimeDate("Y-m-d"))
	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		log.Println("檔案建立失敗", path, err)
		return
	}
	// 延遲關閉檔案
	defer file.Close()
	date := time.Now().Format(time.DateTime)
	msg := fmt.Sprintf(format, args...)
	//經過幾層 呼叫log到這個func經過幾層
	skip := 2
	// 取得當前的檔案路徑和行號
	_, path, line, _ := runtime.Caller(skip)
	// 寫入檔案内容（確保 UTF-8 編碼）
	logLine := fmt.Sprintf("[%s] %s | %v:%v | %s \n", LevelMap[level], date, getRelativePath(path), line, msg)
	if _, err = file.Write([]byte(logLine)); err != nil {
		log.Println("寫入檔案失敗", path, err)
		return
	}
}

// getRelativePath 取得專案內的相對路徑
func getRelativePath(path string) string {
	// 取得專案根目錄
	path = strings.TrimPrefix(path, "/Users/john/workspace/Landtop")
	root, _ := os.Getwd()
	// 去掉根目錄，保留相對路徑
	return strings.TrimPrefix(path, root)
}

// Info 記錄一般日誌，定期自動删除
func Info(format string, args ...interface{}) {
	LogsWrite(LevelInformational, format, args...)
}

// Error 記錄錯誤日誌
func Error(format string, args ...interface{}) {
	LogsWrite(LevelError, format, args...)
}

func Warn(format string, args ...interface{}) {
	LogsWrite(LevelError, format, args...)
}

// Debug 記錄除錯日誌
func Debug(format string, args ...interface{}) {
	LogsWrite(LevelDebug, format, args...)
}
