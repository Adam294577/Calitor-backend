package library

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
)

// AdminTokenClaims JWT token 的 claims 資料
type AdminTokenClaims struct {
	Email    string
	RoleId   *int64
	RoleName *string
}

// GenerateAdminToken 生成管理員 JWT token
func GenerateAdminToken(claimsData AdminTokenClaims) (string, error) {
	jwtSecret := viper.GetString("Server.JwtKey")

	// 設定 token 過期時間（例如 2 天）
	expirationTime := time.Now().Add(2 * 24 * time.Hour)

	// 建立 claims
	claims := jwt.MapClaims{
		"Email":  claimsData.Email,
		"RoleId": claimsData.RoleId,
		"exp":    expirationTime.Unix(),
		"iat":    time.Now().Unix(),
	}

	// 建立 token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 簽名 token
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// ParseAdminToken 解析管理員 JWT token（允許過期）
// 用於刷新 token 時驗證舊 token 的有效性
func ParseAdminToken(tokenString string) (*AdminTokenClaims, error) {
	jwtSecret := viper.GetString("Server.JwtKey")

	// 解析 token（會驗證簽名，但我們允許過期）
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})

	// 如果解析失敗，檢查是否只是過期錯誤
	// 即使 token 過期，只要簽名有效，我們仍然可以提取 claims
	if err != nil {
		errStr := err.Error()
		// 檢查是否包含過期相關的錯誤訊息
		if strings.Contains(errStr, "expired") || strings.Contains(errStr, "Expired") {
			// token 過期但簽名可能有效，如果 token 對象存在則繼續處理
			if token == nil {
				return nil, fmt.Errorf("無效的 Token: %w", err)
			}
			// token 已解析但驗證失敗（可能是過期），繼續處理 claims
		} else {
			// 其他錯誤（如簽名無效）
			return nil, fmt.Errorf("無效的 Token: %w", err)
		}
	}

	// 提取 claims（無論 token 是否過期）
	if token == nil {
		return nil, fmt.Errorf("無法解析 Token")
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		// 提取 Email（用於查詢管理員）
		email, ok := claims["Email"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid token: missing Email")
		}

		// 提取 RoleId（可能是 nil）
		// 注意：刷新 token 時不會使用此值，會重新從資料庫查詢最新的角色資訊
		var roleId *int64
		if roleIdVal, exists := claims["RoleId"]; exists && roleIdVal != nil {
			if roleIdFloat, ok := roleIdVal.(float64); ok {
				roleIdInt := int64(roleIdFloat)
				roleId = &roleIdInt
			}
		}

		// 提取 RoleName（可能是 nil）
		// 注意：刷新 token 時不會使用此值，會重新從資料庫查詢最新的角色資訊
		var roleName *string
		if roleNameVal, exists := claims["RoleName"]; exists && roleNameVal != nil {
			if roleNameStr, ok := roleNameVal.(string); ok {
				roleName = &roleNameStr
			}
		}

		return &AdminTokenClaims{
			Email:    email,
			RoleId:   roleId,
			RoleName: roleName,
		}, nil
	}

	return nil, fmt.Errorf("invalid token claims")
}
