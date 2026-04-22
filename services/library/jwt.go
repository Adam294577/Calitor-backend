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
	AdminId     int64
	Account     string
	RoleIds     []int64
	Permissions []string
}

// GenerateAdminToken 生成管理員 JWT token
func GenerateAdminToken(claimsData AdminTokenClaims) (string, error) {
	jwtSecret := viper.GetString("Server.JwtKey")

	// 設定 token 過期時間（2 天）
	expirationTime := time.Now().Add(2 * 24 * time.Hour)

	// 建立 claims
	claims := jwt.MapClaims{
		"AdminId":     claimsData.AdminId,
		"Account":     claimsData.Account,
		"RoleIds":     claimsData.RoleIds,
		"Permissions": claimsData.Permissions,
		"exp":         expirationTime.Unix(),
		"iat":         time.Now().Unix(),
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
func ParseAdminToken(tokenString string) (*AdminTokenClaims, error) {
	jwtSecret := viper.GetString("Server.JwtKey")

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "expired") || strings.Contains(errStr, "Expired") {
			if token == nil {
				return nil, fmt.Errorf("無效的 Token: %w", err)
			}
		} else {
			return nil, fmt.Errorf("無效的 Token: %w", err)
		}
	}

	if token == nil {
		return nil, fmt.Errorf("無法解析 Token")
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		// 提取 AdminId
		var adminId int64
		if adminIdVal, exists := claims["AdminId"]; exists && adminIdVal != nil {
			if adminIdFloat, ok := adminIdVal.(float64); ok {
				adminId = int64(adminIdFloat)
			}
		}
		if adminId == 0 {
			return nil, fmt.Errorf("invalid token: missing AdminId")
		}

		// 提取 Account
		account, _ := claims["Account"].(string)

		// 提取 RoleIds
		var roleIds []int64
		if roleIdsVal, exists := claims["RoleIds"]; exists && roleIdsVal != nil {
			if roleIdsArr, ok := roleIdsVal.([]interface{}); ok {
				for _, v := range roleIdsArr {
					if f, ok := v.(float64); ok {
						roleIds = append(roleIds, int64(f))
					}
				}
			}
		}

		// 提取 Permissions
		var permissions []string
		if permsVal, exists := claims["Permissions"]; exists && permsVal != nil {
			if permsArr, ok := permsVal.([]interface{}); ok {
				for _, v := range permsArr {
					if s, ok := v.(string); ok {
						permissions = append(permissions, s)
					}
				}
			}
		}

		return &AdminTokenClaims{
			AdminId:     adminId,
			Account:     account,
			RoleIds:     roleIds,
			Permissions: permissions,
		}, nil
	}

	return nil, fmt.Errorf("invalid token claims")
}
