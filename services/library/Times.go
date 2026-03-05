package library

import (
	"fmt"
	"strconv"
	"time"

	"project/services/common"
)

// Now 目前時間
func Now(format string) string {
	switch format {
	case "YmdHis":
		return time.Now().Format("20060102150405")
	case "Y-m-d H-i-s":
		return time.Now().Format("2006-01-02 15:04:05")
	case "Y/m/d H:i:s":
		return time.Now().Format("2006/01/02 15:04:05")
	case "Ymd":
		return time.Now().Format("2006-01-02")
	case "Hms":
		return time.Now().Format("15:04:05")
	case "TwDate":
		year, _ := strconv.Atoi(time.Now().Format("2006"))
		s := common.StringPadLeft(strconv.Itoa(year-1911), 4)
		return fmt.Sprintf("%s%s", s, time.Now().Format("0102"))
	case "TwYear":
		year, _ := strconv.Atoi(time.Now().Format("2006"))
		s := common.StringPadLeft(strconv.Itoa(year-1911), 3)
		return fmt.Sprintf("%s", s)
	default:
		return time.Now().String()
	}
}

// Time  Unix Time 目前時間戳
func Time() int {
	now := time.Now()
	return int(now.Unix())
}

// UnixToTime ...
func UnixToTime(timestamp int64, format string) string {
	t := time.Unix(timestamp, 0).Local() // 或 .UTC()
	return t.Format(format)
}

func FirstAndLastDate(now time.Time) (time.Time, time.Time) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.Add(24*time.Hour - time.Nanosecond)
	return start, end
}

func TodayEnd() time.Time {
	now := time.Now()
	end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
	return end
}
