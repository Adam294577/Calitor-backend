package common

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"math/rand"
	"net/mail"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/google/go-querystring/query"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
)

// NullableString 回傳 *string，空字串則回傳 nil
func NullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Errors 處理錯誤字符串
func Errors(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// JsonEncode 將結構體轉換為 JSON 字符串
func JsonEncode(v interface{}) string {
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

// JsonEncodeNotEscape JSON 編碼且不做 HTML escape
func JsonEncodeNotEscape(v interface{}) string {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return ""
	}
	return buf.String()
}

// Trim 移除字串中的空白、跳脫符號
func Trim(s string) string {
	clean := strings.ReplaceAll(s, " ", "")
	clean = strings.ReplaceAll(clean, "\\", "")
	clean = strings.ReplaceAll(clean, "\t", "")
	clean = strings.ReplaceAll(clean, "\n", "")
	clean = strings.ReplaceAll(clean, "\r", "")
	return clean
}

// BcryptCost 由設定檔控制：dev 1、prod 12（預設 12）
func BcryptCost() int {
	// 如果沒有設定，預設 12
	cost := viper.GetInt("Server.Security.BcryptCost")
	if cost <= 0 {
		return 12
	}
	return cost
}

// HashPassword 產生 bcrypt hash
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost())
	return string(hash), err
}

// CheckPasswordHash 比對明碼與 bcrypt hash
func CheckPasswordHash(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// NormalizeIP 正規化 IP 地址
// 將 IPv6 localhost (::1) 轉換為 IPv4 localhost (127.0.0.1)
// 若為空字串回傳 "unknown"
func NormalizeIP(ip string) string {
	if ip == "::1" || ip == "[::1]" {
		return "127.0.0.1"
	}
	if ip == "" {
		return "unknown"
	}
	return ip
}

// FilePathExist 判斷檔案或檔案路徑是否存在
func FilePathExist(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// IntToString int轉string
func IntToString(input int64) string {
	return strconv.FormatInt(input, 10)
}

// Int64ToString int64 轉 string
func Int64ToString(i int64) string {
	return strconv.Itoa(int(i))
}

// StringPadLeft 左邊補 0
func StringPadLeft(input string, length int) string {
	return fmt.Sprintf("%0*s", length, input)
}

// GetTimeDate 獲取日期時間戳
func GetTimeDate(input string) (date string) {
	if len(input) == 0 {
		input = "YmdHisMS"
	}
	date = input
	// 時區
	timeZone, _ := time.LoadLocation("Asia/Taipei") //ServerInfo["timezone"])
	timer := time.Now().In(timeZone)

	_week := int64(timer.Weekday())
	_ms := timer.UnixNano() / 1e6
	_ns := timer.UnixNano() / 1e9
	msTmp := IntToString(int64(math.Floor(float64(_ms / 1000))))
	nsTmp := IntToString(int64(math.Floor(float64(_ns / 1000000))))

	year := fmt.Sprintf("%0*s", 4, IntToString(int64(timer.Year())))
	month := fmt.Sprintf("%0*s", 2, IntToString(int64(timer.Month())))
	day := fmt.Sprintf("%0*s", 2, IntToString(int64(timer.Day())))
	hour := fmt.Sprintf("%0*s", 2, IntToString(int64(timer.Hour())))
	minute := fmt.Sprintf("%0*s", 2, IntToString(int64(timer.Minute())))
	second := fmt.Sprintf("%0*s", 2, IntToString(int64(timer.Second())))

	week := IntToString(_week)
	WeekZh := [...]string{"日", "一", "二", "三", "四", "五", "六"} // 默认从"日"开始
	Week := WeekZh[_week]
	ms := strings.Replace(IntToString(_ms), msTmp, "", -1)
	ns := strings.Replace(IntToString(_ns), nsTmp, "", -1)

	// 替换关键词
	date = strings.Replace(date, "MS", ms, -1)
	date = strings.Replace(date, "NS", ns, -1)
	date = strings.Replace(date, "Y", year, -1)
	date = strings.Replace(date, "m", month, -1)
	date = strings.Replace(date, "d", day, -1)
	date = strings.Replace(date, "H", hour, -1)
	date = strings.Replace(date, "i", minute, -1)
	date = strings.Replace(date, "s", second, -1)
	date = strings.Replace(date, "W", Week, -1)
	date = strings.Replace(date, "w", week, -1)
	return date
}

// Md5 回傳 md5 雜湊
func Md5(input string) string {
	h := md5.New()
	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil))
}

// JsonToString Json 轉字串（縮排）
func JsonToString(input interface{}) string {
	jsonData, _ := json.MarshalIndent(input, "", " ")
	return string(jsonData)
}

// JsonDecode 將 interface 轉換為 json
func JsonDecode(data []byte, inf interface{}) error {
	if err := json.Unmarshal(data, &inf); err != nil {
		return err
	}
	return nil
}

func JsonToMap(data []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// StructToQuery 將 interface 轉換為 query string
func StructToQuery(message interface{}) (string, error) {
	values, err := query.Values(message)
	if err != nil {
		return "", err
	}
	return values.Encode(), nil
}

// InArray 判斷字串是否在陣列中
func InArray(needArr []string, need string) bool {
	for _, v := range needArr {
		if need == v {
			return true
		}
	}
	return false
}

// InContainsArray 檢查字符串是否包含在陣列中
func InContainsArray(patterns []string, text string) bool {
	for _, v := range patterns {
		if strings.Contains(text, v) {
			return true
		}
	}
	return false
}

// StructToMap 將 struct 轉換成 map
func StructToMap(data interface{}) (map[string]interface{}, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err = json.Unmarshal(jsonData, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// StructToSortedQuery 將 struct 轉排序過的 query string
func StructToSortedQuery(inf interface{}) (string, error) {
	data, err := StructToMap(inf)
	if err != nil {
		return "", err
	}
	params := make(map[string]string)
	for key, value := range data {
		if key == "ATMParam" || key == "CardParam" {
			continue
		}
		params[key] = fmt.Sprintf("%v", value)
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	values := url.Values{}
	for _, key := range keys {
		values.Add(key, params[key])
	}
	return values.Encode(), nil
}

// StructToValues 根據 form tag 轉換成 url.Values
func StructToValues(s interface{}) string {
	values := url.Values{}
	v := reflect.ValueOf(s)
	t := reflect.TypeOf(s)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		formTag := field.Tag.Get("form")
		if formTag == "" {
			continue
		}
		val := v.Field(i).Interface()
		strVal := fmt.Sprintf("%v", val)
		if strVal != "" && strVal != "<nil>" {
			values.Set(formTag, strVal)
		}
	}
	return values.Encode()
}

// DefaultString 如果值空時為預設值
func DefaultString(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

func isZero(value interface{}) bool {
	return reflect.ValueOf(value).IsZero()
}

// DefaultInt 如果值為零則回傳預設值
func DefaultInt(val, def int) int {
	if isZero(val) {
		return def
	}
	return val
}

// StringToInt64 字串轉 int64
func StringToInt64(str string) int64 {
	i, _ := strconv.Atoi(str)
	return int64(i)
}

// StringToInt 字串轉 int
func StringToInt(str string) int {
	i, _ := strconv.Atoi(str)
	return i
}

// StringToFloat64 字串轉 float64
func StringToFloat64(str string) float64 {
	f, _ := strconv.ParseFloat(str, 64)
	return f
}

// RangeNumber 產生亂數數字串
func RangeNumber(max int, length int) string {
	rand.Seed(time.Now().UnixNano())
	randNum := rand.Intn(max)
	num := strconv.Itoa(randNum)
	return fmt.Sprintf("%0*s", length, num)
}

// IsValidEmail 判斷 email 格式
func IsValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// Remove 從 slice 移除指定值
func Remove(slice []string, value string) []string {
	var result []string
	for _, v := range slice {
		if v != value {
			result = append(result, v)
		}
	}
	return result
}

// Round 四捨五入
func Round(x float64) float64 {
	return math.Floor(x + 0.5)
}

// JsonEncodeEscape JSON 編碼並進行 HTML escape
func JsonEncodeEscape(v interface{}) []byte {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(true)
	err := enc.Encode(v)
	if err != nil {
		return nil
	}
	return buf.Bytes()
}

// EncodeQueryDelimiters 將 ? & 轉為跳脫符號
func EncodeQueryDelimiters(s string) string {
	s = strings.ReplaceAll(s, "?", "\u003f")
	s = strings.ReplaceAll(s, "&", "\u0026")
	return s
}

// SafeFloat nil 安全轉換
func SafeFloat(ptr *float64) float64 {
	if ptr == nil {
		return 0
	}
	return *ptr
}

// TemplateReplace 使用模板替換
func TemplateReplace(tmpStr string, data interface{}) (string, error) {
	tmpl, err := template.New("safe").Parse(tmpStr)
	if err != nil {
		return "", fmt.Errorf("解析錯誤: %v", err)
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("執行錯誤: %v", err)
	}
	return buf.String(), nil
}

// CleanBase64Data 清理 base64 數據，移除 data URL 前綴
func CleanBase64Data(data string) string {
	prefixes := []string{
		"data:image/jpeg;base64,",
		"data:image/jpg;base64,",
		"data:image/png;base64,",
		"data:image/gif;base64,",
		"data:image/webp;base64,",
		"data:image/svg+xml;base64,",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(data, prefix) {
			return strings.TrimPrefix(data, prefix)
		}
	}
	return data
}

// DetectImageFormat 從 base64 數據推斷圖片格式
func DetectImageFormat(base64Data string) string {
	if len(base64Data) < 10 {
		return "jpg"
	}
	if strings.HasPrefix(base64Data, "/9j/") {
		return "jpg"
	}
	if strings.HasPrefix(base64Data, "iVBOR") {
		return "png"
	}
	if strings.HasPrefix(base64Data, "R0lGOD") {
		return "gif"
	}
	if strings.HasPrefix(base64Data, "UklGR") {
		return "webp"
	}
	return "jpg"
}

// IntToBool 數字轉 bool
func IntToBool(i int) bool {
	return i != 0
}

func Splice(data []byte, offset []string) string {
	var inf map[string]interface{}
	json.Unmarshal([]byte(data), &inf)
	// 刪除不需要的欄位
	for _, key := range offset {
		delete(inf, key)
	}
	// 轉回 JSON
	resp, _ := json.Marshal(data)
	return string(resp)
}

// safeString 將 *string 轉成 string，nil 時回傳空字串
func SafeString(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

// ToLowerSlug 將 slug 轉為小寫（處理 *string，nil 時回傳 nil）
func ToLowerString(slug *string) *string {
	if slug == nil {
		return nil
	}
	lower := strings.ToLower(*slug)
	return &lower
}

// CalculateSkusStatus 計算 SKU 庫存狀態
// 參數：
//   - skusCount: SKU 總數
//   - skusWithStock: 有庫存的 SKU 數量
//   - availability: 是否上架中（true: 上架中，false: 未上架）
//
// 返回值：
//   - 1: 正常供應（所有 SKU 都有庫存）
//   - 2: 部分供應（部分 SKU 有庫存）
//   - 3: 售完（所有 SKU 都沒有庫存或沒有 SKU）
func CalculateSkusStatus(skusCount, skusWithStock int, availability bool) int {
	// 如果未上架，直接返回售完狀態
	if !availability {
		return 3
	}

	// 沒有 SKU 視為售完
	if skusCount == 0 {
		return 3
	}

	// 計算沒有庫存的 SKU 數量
	skusWithoutStock := skusCount - skusWithStock

	// 所有 SKU 都有庫存
	if skusWithStock > 0 && skusWithoutStock == 0 {
		return 1 // 正常供應
	}

	// 部分 SKU 有庫存
	if skusWithStock > 0 && skusWithoutStock > 0 {
		return 2 // 部分供應
	}

	// 所有 SKU 都沒有庫存
	return 3 // 售完
}

// BuildGormUpdateMap 構建適合 GORM Updates 的 map，確保零值（false、0）也能被更新
// 參數：
//   - data: 要更新的結構體實例（可以是結構體或指針）
//   - fields: 要包含的字段名稱列表（如果為空，則包含所有字段，排除 id、created_at、updated_at）
//
// 返回值：
//   - map[string]interface{}: 適合用於 GORM Updates 的 map
//
// 注意：
//   - 這個函數會將所有字段（包括零值）都包含在返回的 map 中
//   - 使用時直接使用 Updates(map) 即可，GORM 會更新所有在 map 中的字段（包括零值）
func BuildGormUpdateMap(data interface{}, fields []string) map[string]interface{} {
	result := make(map[string]interface{})
	v := reflect.ValueOf(data)
	t := reflect.TypeOf(data)

	// 如果是指針，獲取其元素
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return result
		}
		v = v.Elem()
		t = t.Elem()
	}

	// 如果是指針類型，再次獲取元素（處理 **T 的情況）
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return result
		}
		v = v.Elem()
		t = t.Elem()
	}

	// 如果不是結構體，返回空 map
	if v.Kind() != reflect.Struct {
		return result
	}

	// 排除的字段（這些字段不應該被更新）
	excludedFields := map[string]bool{
		"id":         true,
		"created_at": true,
		"updated_at": true,
	}

	// 構建字段映射表（用於快速查找）
	fieldMap := make(map[string]bool)
	if len(fields) > 0 {
		for _, f := range fields {
			fieldMap[strings.ToLower(f)] = true
		}
	}

	// 遍歷所有字段
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		// 跳過未導出的字段
		if !fieldValue.CanInterface() {
			continue
		}

		// 獲取 gorm tag 中的 column 名稱
		gormTag := field.Tag.Get("gorm")
		var fieldName string

		// 優先使用 gorm tag 中的 column 名稱
		if gormTag != "" && gormTag != "-" {
			// 解析 gorm tag，查找 column:xxx
			for _, part := range strings.Split(gormTag, ";") {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "column:") {
					fieldName = strings.TrimPrefix(part, "column:")
					break
				}
			}
		}

		// 如果沒有找到 column，使用字段名的 snake_case 形式
		if fieldName == "" {
			fieldName = toSnakeCase(field.Name)
		}

		// 跳過排除的字段
		if excludedFields[strings.ToLower(fieldName)] {
			continue
		}

		// 如果指定了字段列表，只處理這些字段
		if len(fields) > 0 {
			fieldNameLower := strings.ToLower(fieldName)
			fieldNameOriginalLower := strings.ToLower(field.Name)
			if !fieldMap[fieldNameLower] && !fieldMap[fieldNameOriginalLower] {
				continue
			}
		}

		// 處理指針類型的字段
		if fieldValue.Kind() == reflect.Ptr {
			if fieldValue.IsNil() {
				result[fieldName] = nil
			} else {
				result[fieldName] = fieldValue.Elem().Interface()
			}
		} else {
			// 非指針類型，直接取值（包括零值）
			result[fieldName] = fieldValue.Interface()
		}
	}

	return result
}

// toSnakeCase 將駝峰命名轉換為 snake_case
func toSnakeCase(str string) string {
	if str == "" {
		return str
	}

	var result []rune
	for i, r := range str {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result = append(result, '_')
		}
		result = append(result, r)
	}
	return strings.ToLower(string(result))
}

// ExtractTextFromHTML 從 HTML 內容中提取純文字
// 移除所有 HTML 標籤、解碼 HTML 實體、去除多餘空白，並限制長度
func ExtractTextFromHTML(htmlContent string, maxLength int) string {
	if htmlContent == "" {
		return ""
	}

	// 移除 <script> 和 <style> 標籤及其內容
	scriptRegex := regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`)
	styleRegex := regexp.MustCompile(`(?i)<style[^>]*>.*?</style>`)
	htmlContent = scriptRegex.ReplaceAllString(htmlContent, "")
	htmlContent = styleRegex.ReplaceAllString(htmlContent, "")

	// 移除所有 HTML 標籤
	tagRegex := regexp.MustCompile(`<[^>]+>`)
	text := tagRegex.ReplaceAllString(htmlContent, " ")

	// 解碼 HTML 實體（如 &amp; -> &, &lt; -> <, &gt; -> >）
	text = html.UnescapeString(text)

	// 將多個空白字元替換為單個空格
	spaceRegex := regexp.MustCompile(`\s+`)
	text = spaceRegex.ReplaceAllString(text, " ")

	// 去除首尾空白
	text = strings.TrimSpace(text)

	// 限制長度
	if maxLength > 0 && len([]rune(text)) > maxLength {
		runes := []rune(text)
		text = string(runes[:maxLength]) + "..."
	}

	return text
}

// GetDeliveryOptionName 根據 delivery_option 值取得對應的名稱
// 參數：
//   - deliveryOption: 配送選項值 (0=宅配到府, 1=門市取貨)
//
// 返回值：
//   - string: 配送選項名稱
func GetDeliveryOptionName(deliveryOption int32) string {
	switch deliveryOption {
	case 0:
		return "宅配到府"
	case 1:
		return "門市取貨"
	default:
		return "未知"
	}
}
